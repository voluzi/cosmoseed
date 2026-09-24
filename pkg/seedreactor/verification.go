package seedreactor

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/cometbft/cometbft/v2/p2p/pex"
)

type verifiedPeer struct {
	addr    *na.NetAddr
	checked time.Time
}

type VerifiedPeer struct {
	Address      string    `json:"address"`
	ID           string    `json:"id"`
	IP           string    `json:"ip"`
	Port         uint16    `json:"port"`
	Verified     bool      `json:"verified"`
	LastVerified time.Time `json:"last_verified"`
	AgeSeconds   float64   `json:"age_seconds"`
}

const maxVerifiedEntries = 4096

type verifiedStore struct {
	mu        sync.RWMutex
	book      pex.AddrBook
	ttl       time.Duration
	peers     map[string]verifiedPeer
	now       func() time.Time
	evictions atomic.Uint64
}

func newVerifiedStore(book pex.AddrBook, ttl time.Duration) *verifiedStore {
	return &verifiedStore{book: book, ttl: ttl, peers: make(map[string]verifiedPeer), now: time.Now}
}

func (s *verifiedStore) record(addr *na.NetAddr) {
	if addr == nil {
		return
	}
	s.mu.Lock()
	key := addr.String()
	for existing, peer := range s.peers {
		if peer.addr.ID == addr.ID && existing != key {
			delete(s.peers, existing)
		}
	}
	if _, exists := s.peers[key]; !exists && len(s.peers) >= maxVerifiedEntries {
		oldestKey := ""
		oldest := s.now()
		for existing, peer := range s.peers {
			if oldestKey == "" || peer.checked.Before(oldest) {
				oldestKey, oldest = existing, peer.checked
			}
		}
		delete(s.peers, oldestKey)
		s.evictions.Add(1)
	}
	s.peers[key] = verifiedPeer{addr: addr, checked: s.now()}
	s.mu.Unlock()
}

func (s *verifiedStore) forget(addr *na.NetAddr) {
	if addr == nil {
		return
	}
	s.mu.Lock()
	delete(s.peers, addr.String())
	s.mu.Unlock()
}

func (s *verifiedStore) forgetID(id string) {
	s.mu.Lock()
	for key, peer := range s.peers {
		if peer.addr.ID == id {
			delete(s.peers, key)
		}
	}
	s.mu.Unlock()
}

func (s *verifiedStore) freshFor(addr *na.NetAddr, duration time.Duration) bool {
	s.mu.RLock()
	entry, ok := s.peers[addr.String()]
	fresh := ok && s.now().Sub(entry.checked) < duration
	s.mu.RUnlock()
	return fresh
}

func (s *verifiedStore) selection() []*na.NetAddr {
	entries := s.freshEntries()
	selected := make([]*na.NetAddr, 0, len(entries))
	for _, entry := range entries {
		selected = append(selected, entry.addr)
	}
	rand.Shuffle(len(selected), func(i, j int) { selected[i], selected[j] = selected[j], selected[i] })
	if len(selected) > 100 {
		selected = selected[:100]
	}
	return selected
}

func (s *verifiedStore) count() int { return len(s.freshEntries()) }

func (s *verifiedStore) freshEntries() []verifiedPeer {
	s.mu.RLock()
	now := s.now()
	snapshot := make(map[string]verifiedPeer, len(s.peers))
	for key, entry := range s.peers {
		snapshot[key] = entry
	}
	s.mu.RUnlock()
	entries := make([]verifiedPeer, 0, len(snapshot))
	stale := make(map[string]verifiedPeer)
	for key, entry := range snapshot {
		if now.Sub(entry.checked) >= s.ttl || !s.book.HasAddress(entry.addr) || s.book.IsBanned(entry.addr) {
			stale[key] = entry
			continue
		}
		entries = append(entries, entry)
	}
	if len(stale) > 0 {
		s.mu.Lock()
		for key, entry := range stale {
			if current, ok := s.peers[key]; ok && current.checked.Equal(entry.checked) {
				delete(s.peers, key)
			}
		}
		s.mu.Unlock()
	}
	return entries
}

func (s *verifiedStore) details() []VerifiedPeer {
	entries := s.freshEntries()
	rand.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
	if len(entries) > 100 {
		entries = entries[:100]
	}
	details := make([]VerifiedPeer, 0, len(entries))
	now := s.now()
	for _, entry := range entries {
		age := now.Sub(entry.checked).Seconds()
		if age < 0 {
			age = 0
		}
		details = append(details, VerifiedPeer{Address: entry.addr.String(), ID: entry.addr.ID, IP: entry.addr.IP.String(), Port: entry.addr.Port, Verified: true, LastVerified: entry.checked, AgeSeconds: age})
	}
	return details
}

type servingBook struct {
	pex.AddrBook
	verified   *verifiedStore
	invalidate func(string)
}

func (b *servingBook) GetSelection() []*na.NetAddr              { return b.verified.selection() }
func (b *servingBook) GetSelectionWithBias(_ int) []*na.NetAddr { return b.verified.selection() }
func (b *servingBook) RemoveAddress(addr *na.NetAddr) {
	b.AddrBook.RemoveAddress(addr)
	if addr != nil {
		b.verified.forgetID(addr.ID)
		if b.invalidate != nil {
			b.invalidate(addr.ID)
		}
	}
}
func (b *servingBook) MarkBad(addr *na.NetAddr, duration time.Duration) {
	b.AddrBook.MarkBad(addr, duration)
	if addr != nil {
		b.verified.forgetID(addr.ID)
		if b.invalidate != nil {
			b.invalidate(addr.ID)
		}
	}
}
