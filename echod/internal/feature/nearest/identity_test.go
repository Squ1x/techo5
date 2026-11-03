package nearest

import (
	"testing"
	"time"
)

func TestCapturedWakeIdentitySurvivesNearestAndGate(t *testing.T) {
	for _, mode := range []string{"immediate", "nearest", "gated"} {
		t.Run(mode, func(t *testing.T) {
			h := newHouse(t)
			h.n.enabled = mode != "immediate"
			var got string
			h.n.woke = func(slot int, _ time.Time, ids ...string) {
				if slot != 1 || len(ids) != 1 {
					t.Fatal("lost detector identity")
				}
				got = ids[0]
			}
			h.n.wokeNow = func(slot int, ids ...string) { h.n.woke(slot, h.now, ids...) }
			// An advertised/configured slot can disagree with the physical detector.
			h.n.wakeWord = func(int) string { return "hey_odin" }
			var release func()
			if mode == "gated" {
				h.n.WokeGated(1, h.now, func(open func()) { release = open }, "galileo")
			} else {
				h.n.Woke(1, "galileo")
			}
			if mode != "immediate" {
				if len(h.sent) != 1 || h.sent[0].Word != "galileo" {
					t.Fatal("nearest claim used a slot instead of the model snapshot")
				}
				h.n.wakeWord = func(int) string { return "another_reloaded_model" }
				h.wait(350 * time.Millisecond)
			}
			if mode == "gated" {
				if release == nil || got != "" {
					t.Fatal("gate did not retain the pending wake")
				}
				release()
			}
			if got != "galileo" {
				t.Fatalf("routed %q instead of captured galileo", got)
			}
		})
	}
}
