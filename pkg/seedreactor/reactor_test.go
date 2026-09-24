package seedreactor

import (
	"testing"
	"time"

	p2papi "github.com/cometbft/cometbft/api/cometbft/p2p/v1"
	"github.com/cometbft/cometbft/v2/config"
	"github.com/cometbft/cometbft/v2/p2p"
	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/cometbft/cometbft/v2/p2p/pex"
)

type outboundTestPeer struct {
	p2p.Peer
	addr *na.NetAddr
	id   string
	sent []p2p.Envelope
}

type attemptCountingBook struct {
	pex.AddrBook
	attempts int
}

func (b *attemptCountingBook) MarkAttempt(addr *na.NetAddr) {
	b.attempts++
	b.AddrBook.MarkAttempt(addr)
}

func (p *outboundTestPeer) IsOutbound() bool          { return true }
func (p *outboundTestPeer) IsRunning() bool           { return true }
func (p *outboundTestPeer) ID() string                { return p.id }
func (p *outboundTestPeer) SocketAddr() *na.NetAddr   { return p.addr }
func (p *outboundTestPeer) Send(e p2p.Envelope) error { p.sent = append(p.sent, e); return nil }

func TestPEXServesOnlyAuthenticatedOutboundPeers(t *testing.T) {
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(addr, addr); err != nil {
		t.Fatal(err)
	}
	book.MarkGood(addr.ID)
	r := NewReactor(book, nil, 10, 1, false, time.Hour, time.Minute, 5, time.Hour)
	requester := &outboundTestPeer{addr: addr, id: "other-requester"}
	r.Receive(p2p.Envelope{Src: requester, Message: &p2papi.PexRequest{}})
	if got := lastPEXAddrs(t, requester); got != 0 {
		t.Fatalf("raw good peer leaked via PEX: %d", got)
	}
	peer := &outboundTestPeer{addr: addr, id: addr.ID}
	r.AddPeer(peer)
	requester = &outboundTestPeer{addr: addr, id: "third-requester"}
	r.Receive(p2p.Envelope{Src: requester, Message: &p2papi.PexRequest{}})
	if got := lastPEXAddrs(t, requester); got != 1 {
		t.Fatalf("verified peer missing via PEX: %d", got)
	}
}

func lastPEXAddrs(t *testing.T, peer *outboundTestPeer) int {
	t.Helper()
	for i := len(peer.sent) - 1; i >= 0; i-- {
		if msg, ok := peer.sent[i].Message.(*p2papi.PexAddrs); ok {
			return len(msg.Addrs)
		}
	}
	t.Fatal("no PEX response")
	return 0
}

func TestCandidateQueueIsBoundedAndDeduplicated(t *testing.T) {
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(addr, addr); err != nil {
		t.Fatal(err)
	}
	r := NewReactor(book, nil, 1, 1, false, time.Hour, time.Minute, 5, time.Hour)
	r.queueCandidates()
	r.queueCandidates()
	if got := len(r.addrChan); got != 1 {
		t.Fatalf("queued %d duplicate candidates", got)
	}
}

func TestExpiredBanReturnsOnlyAsUnverifiedCandidate(t *testing.T) {
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(addr, addr); err != nil {
		t.Fatal(err)
	}
	r := NewReactor(book, nil, 10, 1, false, time.Hour, time.Minute, 5, time.Hour)
	r.verified.record(addr)
	r.serving.MarkBad(addr, time.Millisecond)
	if got := r.VerifiedCount(); got != 0 {
		t.Fatalf("banned peer served: %d", got)
	}
	time.Sleep(5 * time.Millisecond)
	r.recheck()
	if got := r.VerifiedCount(); got != 0 {
		t.Fatalf("reinstated peer inherited verification: %d", got)
	}
	if len(r.addrChan) != 1 {
		t.Fatalf("reinstated peer was not queued: %d", len(r.addrChan))
	}
}

func TestAuthenticatedEndpointReplacesCandidateForSameID(t *testing.T) {
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	old, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.2:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(old, old); err != nil {
		t.Fatal(err)
	}
	r := NewReactor(book, nil, 10, 1, false, time.Hour, time.Minute, 5, time.Hour)
	r.AddPeer(&outboundTestPeer{addr: replacement, id: replacement.ID})
	selection := book.GetSelection()
	if len(selection) != 1 || selection[0].String() != replacement.String() {
		t.Fatalf("candidate endpoint = %v", selection)
	}
	if got := r.VerifiedCount(); got != 1 {
		t.Fatalf("verified endpoint count = %d", got)
	}
}

func TestConnectedOutboundPeerRefreshesBeforeTTLExpiry(t *testing.T) {
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	r := NewReactor(book, nil, 10, 1, false, time.Hour, time.Minute, 5, time.Hour)
	now := time.Now()
	r.verified.now = func() time.Time { return now }
	peer := &outboundTestPeer{addr: addr, id: addr.ID}
	r.AddPeer(peer)
	now = now.Add(45 * time.Minute)
	r.refreshConnectedPeers([]p2p.Peer{peer})
	now = now.Add(30 * time.Minute)
	if got := r.VerifiedCount(); got != 1 {
		t.Fatalf("connected outbound proof expired: %d", got)
	}
}

func TestFullOutboundCapacityLeavesCandidateEligible(t *testing.T) {
	book := &attemptCountingBook{AddrBook: pex.NewAddrBook(t.TempDir()+"/book.json", false)}
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(addr, addr); err != nil {
		t.Fatal(err)
	}
	r := NewReactor(book, nil, 1, 1, false, time.Hour, time.Minute, 1, time.Hour)
	cfg := config.DefaultP2PConfig()
	cfg.MaxNumOutboundPeers = 0
	r.SetSwitch(p2p.NewSwitch(cfg, nil))
	now := time.Now()
	r.verified.now = func() time.Time { return now }
	r.verified.record(addr)
	now = now.Add(45 * time.Minute)
	r.queueCandidates()
	if got := len(r.addrChan); got != 1 {
		t.Fatalf("initial queue depth = %d", got)
	}
	r.StartDialWorkers(1)
	defer func() { close(r.quitCh); r.workers.Wait() }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		r.pendingMu.Lock()
		pending := r.pending[addr.String()]
		r.pendingMu.Unlock()
		if !pending && len(r.addrChan) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dial worker did not clear pending address")
		}
		time.Sleep(time.Millisecond)
	}

	if stats := r.Stats(); stats.Attempts != 0 || stats.DialFailures != 0 {
		t.Fatalf("capacity skip counted as dial: %+v", stats)
	}
	if book.attempts != 0 {
		t.Fatalf("address book attempts = %d", book.attempts)
	}
	if len(r.failures) != 0 {
		t.Fatalf("capacity skip recorded failures: %v", r.failures)
	}
	if book.IsBanned(addr) || !book.HasAddress(addr) {
		t.Fatal("capacity skip removed or banned address")
	}
	if got := r.VerifiedCount(); got != 1 {
		t.Fatalf("capacity skip lost verification: %d", got)
	}
	r.queueCandidates()
	if got := r.Stats().Queued; got != 2 {
		t.Fatalf("candidate not eligible on recheck: queued = %d", got)
	}
}
