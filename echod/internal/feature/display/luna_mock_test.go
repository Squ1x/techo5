//go:build mockup && !dot && !spot

package display

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// MOCK_OUT=/path go test -tags mockup ./internal/feature/display -run TestMockLuna
// Images come from the real renderer, not a second implementation of the design.
func TestMockLuna(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	previewFont(t)
	for _, err := range []error{config.Set().Screen().Skin(skinLuna), config.Set().Screen().ClockStyle(styleLuna), config.Set().Screen().TimerStyle(timerLuna), config.Set().Screen().Language("de")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	old := clock24.Load()
	clock24.Store(true)
	t.Cleanup(func() { clock24.Store(old) })
	applyTheme(current())
	applySkin(nil, 0, 0)
	at := time.Date(2026, 11, 3, 21, 7, 0, 0, time.Local)
	today := func(hour, min int) time.Time { return time.Date(2026, 11, 3, hour, min, 0, 0, time.Local) }
	tomorrow := func(hour, min int) time.Time { return today(hour, min).AddDate(0, 0, 1) }
	s := scene{now: at, phase: "idle", weather: home.Weather{Condition: "cloudy", Temp: "12°"},
		timers: []timer.Countdown{{Name: "Nudeln", Left: 272 * time.Second, Total: 439 * time.Second, Active: true}},
		style: styleFacts{days: []hass.Day{{High: 14, Low: 6}}, next: []hass.Event{
			{Summary: "Zahnarzt", Start: today(15, 30)}, {Summary: "Training", Start: today(18, 0)},
			{Summary: "Standup", Start: tomorrow(9, 0)}, {Summary: "Müll Papier", Start: tomorrow(0, 0), AllDay: true},
		}},
	}
	out := os.Getenv("MOCK_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"01-idle", "01b-idle-shiva", "06-timer-ringing"} {
		img := image.NewRGBA(image.Rect(0, 0, 960, 480))
		r := newRenderer(img)
		sc := s
		if name == "06-timer-ringing" {
			sc.ring = ringState{timer: "Nudeln"}
			sc.timers = nil
		}
		if name == "01b-idle-shiva" {
			// Tick 86 gives x=235.5, frame 6. The mock artist placed frame 1 at
			// x=236; production ties frame and x to real elapsed time instead.
			r.luna.animator.random = func() float64 { return 0 }
			r.luna.animator.nextWalk = sc.now.Add(time.Hour)
			r.luna.animator.nextBlink = sc.now.Add(time.Hour)
			r.luna.animator.walk = sc.now.Add(-86 * lunaFrame)
		}
		r.draw(sc)
		mockSave(t, out, name, img)
	}
}
