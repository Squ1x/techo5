package speaker

import (
	"context"
	"testing"
	"time"
)

func TestCancelClaimDoesNotDrainItsReplacement(t *testing.T) {
	d := driver()
	started := make(chan struct{})
	old := d.Claim("Luna terminal", func(ctx context.Context, p *Player) error {
		p.mu.Lock()
		p.pending = make([]int16, 1920) // no ALSA device: seed the queue directly
		p.mu.Unlock()
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	d.CancelClaim(old)
	waitFor(t, old)
	if !old.Stopped() || d.p.Queued() != 0 {
		t.Fatal("cancel did not stop/drain the current claim")
	}
	started = make(chan struct{})
	replacement := d.Claim("call", func(ctx context.Context, p *Player) error {
		p.mu.Lock()
		p.pending = make([]int16, 1920)
		p.mu.Unlock()
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	d.CancelClaim(old)
	if d.Holder() != "call" || d.p.Queued() == 0 {
		t.Fatal("old Luna stop drained its replacement")
	}
	select {
	case <-replacement.Done():
		t.Fatal("replacement canceled")
	case <-time.After(10 * time.Millisecond):
	}
	d.CancelClaim(nil)
	d.CancelClaim(replacement)
	waitFor(t, replacement)
}
