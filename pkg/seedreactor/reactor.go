package seedreactor

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	p2papi "github.com/cometbft/cometbft/api/cometbft/p2p/v1"
	"github.com/cometbft/cometbft/v2/libs/log"
	"github.com/cometbft/cometbft/v2/p2p"
	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/cometbft/cometbft/v2/p2p/pex"
)

type SeedReactor struct {
	*pex.Reactor

	book             pex.AddrBook
	serving          *servingBook
	verified         *verifiedStore
	log              log.Logger
	addrChan         chan *AddrPair
	quitCh           chan struct{}
	stopOnce         sync.Once
	workers          sync.WaitGroup
	pendingMu        sync.Mutex
	pending          map[string]bool
	failures         map[string]int
	failureAddresses map[string]*na.NetAddr
	failureChecked   map[string]time.Time
	dialWorkers      int
	strict           bool
	recheckInterval  time.Duration
	maxDialFailures  int
	banDuration      time.Duration
	queued           atomic.Uint64
	dropped          atomic.Uint64
	attempts         atomic.Uint64
	dialFailures     atomic.Uint64
	authenticated    atomic.Uint64
	received         atomic.Uint64
}

const maxFailureEntries = 4096

type Stats struct {
	QueueDepth    int
	QueueCapacity int
	Queued        uint64
	Dropped       uint64
	Attempts      uint64
	DialFailures  uint64
	Authenticated uint64
	Received      uint64
	Evictions     uint64
}

func (s *SeedReactor) Stats() Stats {
	return Stats{len(s.addrChan), cap(s.addrChan), s.queued.Load(), s.dropped.Load(), s.attempts.Load(), s.dialFailures.Load(), s.authenticated.Load(), s.received.Load(), s.verified.evictions.Load()}
}

type AddrPair struct {
	Addr   *na.NetAddr
	Source *na.NetAddr
}

func NewReactor(book pex.AddrBook, seeds []string, queueSize, dialWorkers int, strict bool, ttl, recheckInterval time.Duration, maxDialFailures int, banDuration time.Duration) *SeedReactor {
	verified := newVerifiedStore(book, ttl)
	serving := &servingBook{AddrBook: book, verified: verified}
	r := pex.NewReactor(serving, &pex.ReactorConfig{
		SeedMode:          true,
		Seeds:             seeds,
		EnsurePeersPeriod: 30 * time.Second,
	})

	seed := &SeedReactor{
		Reactor:          r,
		book:             book,
		serving:          serving,
		verified:         verified,
		log:              log.NewNopLogger(),
		addrChan:         make(chan *AddrPair, queueSize),
		quitCh:           make(chan struct{}),
		dialWorkers:      dialWorkers,
		strict:           strict,
		recheckInterval:  recheckInterval,
		maxDialFailures:  maxDialFailures,
		banDuration:      banDuration,
		pending:          make(map[string]bool),
		failures:         make(map[string]int),
		failureAddresses: make(map[string]*na.NetAddr),
		failureChecked:   make(map[string]time.Time),
	}
	serving.invalidate = seed.invalidateID
	return seed
}

func (s *SeedReactor) invalidateID(id string) {
	s.pendingMu.Lock()
	for key, addr := range s.failureAddresses {
		if addr.ID == id {
			delete(s.failures, key)
			delete(s.failureAddresses, key)
			delete(s.failureChecked, key)
		}
	}
	for key := range s.pending {
		if strings.HasPrefix(key, id+"@") {
			delete(s.pending, key)
		}
	}
	s.pendingMu.Unlock()
}

func (s *SeedReactor) Start() error {
	if err := s.Reactor.Start(); err != nil {
		return err
	}
	s.StartDialWorkers(s.dialWorkers)
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		ticker := time.NewTicker(s.recheckInterval)
		defer ticker.Stop()
		s.recheck()
		for {
			select {
			case <-s.quitCh:
				return
			case <-ticker.C:
				s.recheck()
			}
		}
	}()
	return nil
}

func (s *SeedReactor) Stop() error {
	var err error
	s.stopOnce.Do(func() { close(s.quitCh); err = s.Reactor.Stop(); s.workers.Wait() })
	return err
}

func (s *SeedReactor) SetLogger(logger log.Logger) {
	s.log = logger
	s.Reactor.SetLogger(logger)
}

func (s *SeedReactor) ServingBook() pex.AddrBook { return s.serving }
func (s *SeedReactor) VerifiedCount() int        { return s.verified.count() }

func (s *SeedReactor) recheck() {
	s.book.ReinstateBadPeers()
	s.verified.count()
	s.pruneFailures()
	if s.Switch != nil {
		s.refreshConnectedPeers(s.Switch.Peers().Copy())
	}
	s.queueCandidates()
}

func (s *SeedReactor) refreshConnectedPeers(peers []p2p.Peer) {
	for _, peer := range peers {
		if peer == nil || !peer.IsRunning() || !peer.IsOutbound() {
			continue
		}
		addr := peer.SocketAddr()
		if addr == nil || addr.ID != peer.ID() || (s.strict && !addr.Routable()) {
			continue
		}
		if !s.verified.freshFor(addr, s.verified.ttl/2) {
			s.recordAuthenticated(addr)
		}
	}
}

func (s *SeedReactor) pruneFailures() {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	for key, addr := range s.failureAddresses {
		if time.Since(s.failureChecked[key]) >= s.verified.ttl || !s.book.HasAddress(addr) || s.book.IsBanned(addr) {
			delete(s.failures, key)
			delete(s.failureAddresses, key)
			delete(s.failureChecked, key)
		}
	}
}

func (s *SeedReactor) AddPeer(p p2p.Peer) {
	s.Reactor.AddPeer(p)
	if !p.IsOutbound() {
		return
	}
	addr := p.SocketAddr()
	if addr == nil || addr.ID != p.ID() {
		s.log.Warn("not adding peer: no address", "id", p.ID())
		return
	}
	if s.strict && !addr.Routable() {
		s.log.Warn("not adding peer: address not routable", "id", p.ID(), "addr", addr)
		return
	}

	if s.recordAuthenticated(addr) {
		s.authenticated.Add(1)
	}
}

func (s *SeedReactor) recordAuthenticated(addr *na.NetAddr) bool {
	if !s.verified.freshFor(addr, s.verified.ttl) {
		// Install the authenticated endpoint before MarkGood makes its ID immutable to alternate gossip.
		s.serving.RemoveAddress(addr)
		if err := s.serving.AddAddress(addr, addr); err != nil {
			s.log.Debug("unable to add authenticated peer", "err", err)
			return false
		}
	}
	s.book.MarkGood(addr.ID)
	s.verified.record(addr)
	s.pendingMu.Lock()
	for key, failedAddr := range s.failureAddresses {
		if failedAddr.ID == addr.ID {
			delete(s.failures, key)
			delete(s.failureAddresses, key)
			delete(s.failureChecked, key)
		}
	}
	s.pendingMu.Unlock()
	return true
}

func (s *SeedReactor) Receive(e p2p.Envelope) {
	s.Reactor.Receive(e)
	if _, ok := e.Message.(*p2papi.PexAddrs); ok {
		s.received.Add(uint64(len(e.Message.(*p2papi.PexAddrs).Addrs)))
		s.queueCandidates()
	}
}

func (s *SeedReactor) StartDialWorkers(n int) {
	for i := 0; i < n; i++ {
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			for {
				select {
				case addr := <-s.addrChan:
					s.log.With("dial-worker", i).Debug("dialing peer", "peer", addr)
					s.processAddr(addr)
					if addr != nil && addr.Addr != nil {
						s.pendingMu.Lock()
						delete(s.pending, addr.Addr.String())
						s.pendingMu.Unlock()
					}
				case <-s.quitCh:
					return
				}
			}
		}()
	}
}

func (s *SeedReactor) processAddr(addr *AddrPair) {
	if addr == nil || addr.Addr == nil {
		s.log.Debug("ignoring nil address")
		return
	}
	s.pendingMu.Lock()
	pending := s.pending[addr.Addr.String()]
	s.pendingMu.Unlock()
	if !pending {
		return
	}

	if s.strict && !addr.Addr.Routable() {
		s.log.Debug("received peer address not routable. Ignoring", "addr", addr.Addr.DialString())
		return
	}

	if s.Switch.IsDialingOrExistingAddress(addr.Addr) {
		s.log.Debug("already dialing or connected", "addr", addr)
		return
	}
	outbound, _, dialing := s.Switch.NumPeers()
	if outbound+dialing >= s.Switch.MaxNumOutboundPeers() {
		s.log.Debug("outbound peer capacity reached", "addr", addr)
		return
	}
	s.attempts.Add(1)
	err := s.Switch.DialPeerWithAddress(addr.Addr)
	if err != nil {
		s.dialFailures.Add(1)
		s.log.Debug("dial failed", "addr", addr, "err", err)
		s.book.MarkAttempt(addr.Addr)
		s.verified.forget(addr.Addr)
		s.pendingMu.Lock()
		key := addr.Addr.String()
		if _, exists := s.failures[key]; !exists && len(s.failures) >= maxFailureEntries {
			for old := range s.failures {
				delete(s.failures, old)
				delete(s.failureAddresses, old)
				delete(s.failureChecked, old)
				break
			}
		}
		s.failures[key]++
		s.failureAddresses[key] = addr.Addr
		s.failureChecked[key] = time.Now()
		if s.failures[key] >= s.maxDialFailures {
			delete(s.failures, key)
			delete(s.failureAddresses, key)
			delete(s.failureChecked, key)
			s.pendingMu.Unlock()
			s.serving.MarkBad(addr.Addr, s.banDuration)
			return
		}
		s.pendingMu.Unlock()
		return
	}
}

func (s *SeedReactor) GetPeerSelection() []*na.NetAddr {
	return s.verified.selection()
}

func (s *SeedReactor) GetPeerDetails() []VerifiedPeer { return s.verified.details() }

func (s *SeedReactor) queueCandidates() {
	for _, addr := range s.book.GetSelection() {
		if addr == nil || (s.strict && !addr.Routable()) || s.book.IsBanned(addr) {
			continue
		}
		if s.verified.freshFor(addr, s.verified.ttl/2) {
			continue
		}
		s.pendingMu.Lock()
		key := addr.String()
		if s.pending[key] {
			s.pendingMu.Unlock()
			continue
		}
		s.pending[key] = true
		s.pendingMu.Unlock()
		select {
		case s.addrChan <- &AddrPair{Addr: addr}:
			s.queued.Add(1)
		default:
			s.dropped.Add(1)
			s.pendingMu.Lock()
			delete(s.pending, key)
			s.pendingMu.Unlock()
			return
		}
	}
}
