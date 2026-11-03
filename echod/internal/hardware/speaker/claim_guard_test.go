package speaker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/luna"
)

func TestDisplacedOrdinaryAcquisitionReturnsStoppedHandle(t *testing.T) {
	d := NewDriver(&Player{})
	bg := heldBackground{make(chan struct{}), make(chan struct{})}
	d.Yields(bg)
	returned := make(chan *Claim, 1)
	go func() {
		returned <- d.Claim("pending", func(context.Context, *Player) error {
			t.Error("displaced acquisition started playback")
			return nil
		})
	}()
	<-bg.entered
	phone := d.ClaimCall("phone", func(ctx context.Context, _ *Player) error { <-ctx.Done(); return ctx.Err() })
	defer d.CancelClaim(phone)
	d.p.mu.Lock()
	d.p.pending = make([]int16, 1920)
	d.p.mu.Unlock()
	close(bg.release)
	select {
	case claim := <-returned:
		if claim == nil || !claim.Stopped() || !claim.Finished() {
			t.Fatal("ordinary caller lost its stopped handle")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("acquisition did not finish")
	}
	if d.Holder() != "phone" || d.p.Queued() != 960 {
		t.Fatal("displaced acquisition changed phone output")
	}
}

func TestCanceledLunaAcquisitionCannotDisplacePhone(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Media().OnTurn(config.OnTurnPause); err != nil {
		t.Fatal(err)
	}
	// No background-duck lookup: acquisition blocks at the ownership mutex itself.
	d := NewDriver(&Player{})
	entered, returned := make(chan struct{}), make(chan *Claim, 1)
	d.mu.Lock()
	output := luna.NewOutput(make(chan struct{}, 1), func(generation context.Context, frames <-chan []int16) func() {
		close(entered)
		claim := d.ClaimSpeechIf(func() bool { return generation.Err() == nil }, "Luna terminal", func(ctx context.Context, p *Player) error {
			t.Error("canceled generation started playback")
			return nil
		})
		returned <- claim
		return func() { d.CancelClaim(claim) }
	})
	<-entered
	// Acquisition is blocked inside the real Driver (not in an artificial factory).
	// Reset invalidates it before a live phone becomes the owner under the same lock.
	output.Reset()
	phoneCtx, cancelPhone := context.WithCancel(context.Background())
	defer cancelPhone()
	phone := &Claim{name: "phone", call: true, cancel: cancelPhone, done: make(chan struct{})}
	d.now = phone
	d.p.mu.Lock()
	d.p.pending = make([]int16, 1920)
	d.p.mu.Unlock()
	d.mu.Unlock()
	defer output.Reset()
	select {
	case claim := <-returned:
		if claim != nil {
			t.Fatal("canceled Luna acquired speaker ownership")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("acquisition did not return")
	}
	if phone.Stopped() || phone.Preempted() || phoneCtx.Err() != nil {
		t.Fatal("stale Luna acquisition stopped phone")
	}
	if d.Holder() != "phone" || d.p.Queued() != 960 {
		t.Fatal("stale Luna displaced/drained phone")
	}
}
