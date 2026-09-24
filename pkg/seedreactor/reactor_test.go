package seedreactor

import (
	"testing"
	"time"

	p2papi "github.com/cometbft/cometbft/api/cometbft/p2p/v1"
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

func (p *outboundTestPeer) IsOutbound() bool          { return true }
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
