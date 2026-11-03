// Only native playback/arbiter surfaces are stubbed. The runner copies the real
// driver.go and claim_guard_test.go unchanged; config and luna.Output are real.
package speaker

import "sync"

const (
	Rate      = 48000
	Channels  = 2
	period    = 960
	periods   = 3
	RoleMedia = iota
	RoleCall
	RoleVoice
)

type Player struct {
	mu      sync.Mutex
	pending []int16
}

func Get() *Player            { return &Player{} }
func (*Player) Hold(bool)     {}
func (*Player) SetRole(int)   {}
func (p *Player) Drain()      { p.mu.Lock(); defer p.mu.Unlock(); p.pending = nil }
func (p *Player) Queued() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.pending) / Channels }

type Arbiter struct{}

func (*Arbiter) duckTo(string, int, bool) {}
