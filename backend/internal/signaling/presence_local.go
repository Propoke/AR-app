package signaling

import "sync"

// LocalPresence tracks room membership in memory. Counts (not booleans) tolerate a
// brief overlap when a peer reconnects before the old socket is reaped.
type LocalPresence struct {
	mu    sync.Mutex
	rooms map[string]map[Role]int
}

// NewLocalPresence constructs an in-memory presence tracker.
func NewLocalPresence() *LocalPresence {
	return &LocalPresence{rooms: make(map[string]map[Role]int)}
}

func (p *LocalPresence) Join(room string, role Role) []Role {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rooms[room] == nil {
		p.rooms[room] = make(map[Role]int)
	}
	p.rooms[room][role]++

	var others []Role
	for r, n := range p.rooms[room] {
		if r != role && n > 0 {
			others = append(others, r)
		}
	}
	return others
}

func (p *LocalPresence) Leave(room string, role Role) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := p.rooms[room]
	if counts == nil {
		return true // already gone; treat as empty
	}
	if counts[role] > 0 {
		counts[role]--
	}
	if counts[role] <= 0 {
		delete(counts, role)
	}
	if len(counts) == 0 {
		delete(p.rooms, room)
		return true
	}
	return false
}

func (p *LocalPresence) Close() error { return nil }
