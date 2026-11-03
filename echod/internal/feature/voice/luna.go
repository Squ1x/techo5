package voice

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/luna"
	"github.com/HuskerMinion/techo5/echod/internal/feature/mute"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// Luna owns endpointing and follow-up in a persistent room. The existing HA/direct
// backend interface owns a single locally-ended turn, so this mode is selected at
// Voice's shared wake/action boundary, not by duplicating capture or wake detection.
func newLuna() *luna.Client {
	cfg := config.Get().Luna
	if !cfg.Enabled() {
		return nil
	}
	var client *luna.Client
	var state State
	var turn uint64
	var output *luna.Output
	acquiring := make(chan struct{}, 1)
	reset := func() {
		if output != nil {
			output.Reset()
			output = nil
		}
	}
	client = luna.New(luna.Config{URL: cfg.URL, Token: cfg.Token}, luna.Callbacks{
		AllowAudio: lunaMicAllowed,
		Reset:      reset,
		Event: func(e luna.Event) {
			if e.Turn > turn || (e.Type == "state" && e.State == "listening" && state.Phase == "idle") {
				state = State{}
				turn = e.Turn
			}
			switch e.Type {
			case "state":
				switch e.State {
				case "listening", "followup":
					state.Phase = "listening"
				case "processing":
					state.Phase = "thinking"
				case "speaking", "ringing":
					state.Phase = "replying"
				default:
					state.Phase = "idle"
				}
			case "transcript":
				state.Heard = boundedLunaText(e.Text, 8192)
			case "assistant":
				state.Reply = boundedLunaText(state.Reply+e.Text, 32768)
			default:
				return // Do not expose raw tool data/arguments on the display.
			}
			Changed.Emit(state)
		},
		Play: func(pcm []int16) {
			if !lunaMicAllowed() {
				return
			}
			if output == nil {
				// The terminal clocks silence even while listening/sleeping. It must
				// not claim the speaker (or displace a call) just to play those zeros.
				nonzero := false
				for _, sample := range pcm {
					if sample != 0 {
						nonzero = true
						break
					}
				}
				if !nonzero {
					return
				}
				output = luna.NewOutput(acquiring, func(generation context.Context, frames <-chan []int16) func() {
					var outputMu sync.Mutex
					claim := speaker.Sound().ClaimSpeechIf(func() bool { return generation.Err() == nil }, "Luna terminal", func(ctx context.Context, p *speaker.Player) error {
						defer func() {
							if ctx.Err() != nil {
								go client.StopIf(func() bool { return generation.Err() == nil })
							}
						}()
						for {
							select {
							case <-ctx.Done():
								return ctx.Err()
							case frame := <-frames:
								outputMu.Lock()
								if generation.Err() == nil && ctx.Err() == nil && p.Queued() < 48000/10 {
									p.Play(frame)
								}
								outputMu.Unlock()
							}
						}
					})
					return func() { outputMu.Lock(); defer outputMu.Unlock(); speaker.Sound().CancelClaim(claim) }
				})
			}
			output.Play(pcm)
		},
	})
	return client
}
func lunaMicAllowed() bool { return !micTaken() && luna.MicAllowed(mute.Get()) }

func boundedLunaText(s string, n int) string {
	if len(s) > n {
		return strings.ToValidUTF8(s[:n], "")
	}
	return s
}
func (v *Voice) runLuna(ctx context.Context) error {
	frames, unlisten := mic.Get().Listen("Luna terminal")
	defer unlisten()
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-tick.C:
				if !lunaMicAllowed() {
					v.luna.Stop()
				}
			case frame, ok := <-frames:
				if !ok {
					return
				}
				if lunaMicAllowed() {
					v.luna.Audio(frame)
				} else {
					v.luna.Stop()
				}
			}
		}
	}()
	err := v.luna.Run(child)
	cancel()
	<-done
	return err
}
