package seedreactor

import (
	"fmt"
	"testing"
	"time"

	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/cometbft/cometbft/v2/p2p/pex"
)

func TestVerifiedPeersExpireWithoutScheduler(t *testing.T) {
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	now := time.Now()
	store := newVerifiedStore(book, 10*time.Minute)
	store.now = func() time.Time { return now }
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(addr, addr); err != nil {
		t.Fatal(err)
	}
	store.record(addr)
	if got := store.selection(); len(got) != 1 {
		t.Fatalf("fresh selection = %v", got)
	}
	now = now.Add(11 * time.Minute)
	if got := store.selection(); len(got) != 0 {
		t.Fatalf("expired selection = %v", got)
	}
}

type idOnlyBook struct {
	pex.AddrBook
	ids map[string]bool
}

func (b *idOnlyBook) HasAddress(addr *na.NetAddr) bool     { return b.ids[addr.ID] }
func (b *idOnlyBook) IsBanned(*na.NetAddr) bool            { return false }
func (b *idOnlyBook) RemoveAddress(addr *na.NetAddr)       { delete(b.ids, addr.ID) }
func (b *idOnlyBook) AddAddress(addr, _ *na.NetAddr) error { b.ids[addr.ID] = true; return nil }

func TestVerifiedCountIsUncappedWhilePEXSelectionIsCapped(t *testing.T) {
	book := &idOnlyBook{ids: map[string]bool{}}
	store := newVerifiedStore(book, time.Hour)
	for i := 1; i <= 101; i++ {
		addr, err := na.NewFromString(fmt.Sprintf("%040x@127.0.0.1:%d", i, 20000+i))
		if err != nil {
			t.Fatal(err)
		}
		book.ids[addr.ID] = true
		store.record(addr)
	}
	if got := store.count(); got != 101 {
		t.Fatalf("fresh count = %d", got)
	}
	if got := len(store.selection()); got != 100 {
		t.Fatalf("PEX selection = %d", got)
	}
}

func TestVerificationDoesNotTransferAcrossEndpoints(t *testing.T) {
	book := &idOnlyBook{ids: map[string]bool{}}
	store := newVerifiedStore(book, time.Hour)
	old, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	newAddr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.2:26656")
	if err != nil {
		t.Fatal(err)
	}
	book.ids[old.ID] = true
	store.record(old)
	serving := &servingBook{AddrBook: book, verified: store}
	serving.RemoveAddress(old)
	if err := serving.AddAddress(newAddr, newAddr); err != nil {
		t.Fatal(err)
	}
	if got := store.count(); got != 0 {
		t.Fatalf("replacement inherited verification: %d", got)
	}
	store.record(newAddr)
	selected := store.selection()
	if len(selected) != 1 || selected[0].String() != newAddr.String() {
		t.Fatalf("replacement selection = %v", selected)
	}
}

func TestVerifiedMetadataPrunesAndRemainsBounded(t *testing.T) {
	book := &idOnlyBook{ids: map[string]bool{}}
	store := newVerifiedStore(book, time.Hour)
	now := time.Now()
	store.now = func() time.Time { return now }
	for i := 1; i <= maxVerifiedEntries+1; i++ {
		addr, err := na.NewFromString(fmt.Sprintf("%040x@127.0.0.1:%d", i, 10000+i))
		if err != nil {
			t.Fatal(err)
		}
		book.ids[addr.ID] = true
		store.record(addr)
	}
	if len(store.peers) > maxVerifiedEntries {
		t.Fatalf("unbounded metadata: %d", len(store.peers))
	}
	now = now.Add(2 * time.Hour)
	if got := store.count(); got != 0 {
		t.Fatalf("expired count = %d", got)
	}
	if len(store.peers) != 0 {
		t.Fatalf("expired metadata retained: %d", len(store.peers))
	}
}

func TestPrunedPeerIsNotServed(t *testing.T) {
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(addr, addr); err != nil {
		t.Fatal(err)
	}
	store := newVerifiedStore(book, time.Hour)
	store.record(addr)
	book.RemoveAddress(addr)
	if got := store.selection(); len(got) != 0 {
		t.Fatalf("pruned peer served: %v", got)
	}
}
