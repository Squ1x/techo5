//go:build !dot && !spot

package display

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
)

func TestLunaIdleAnimator(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, random := range []float64{0, 0.5, 1} {
		a := lunaIdleAnimator{random: func() float64 { return random }}
		a.step(now, true)
		want := time.Hour + time.Duration(random*float64(time.Hour))
		if a.nextWalk.Sub(now) != want {
			t.Fatalf("walk after %v, want %v", a.nextWalk.Sub(now), want)
		}
		start := a.nextWalk
		a.step(start, true)
		if x, frame, ok := a.sprite(start); !ok || x != -130 || frame != 0 {
			t.Fatalf("start %d %d %t", x, frame, ok)
		}
		if x, frame, ok := a.sprite(start.Add(8 * lunaFrame)); !ok || x != -96 || frame != 0 {
			t.Fatalf("8 ticks %d %d %t", x, frame, ok)
		}
		a.step(start.Add(257*lunaFrame), true)
		if !a.walk.IsZero() {
			t.Fatal("walk must end after 25.7s")
		}
		a.walk = start
		a.step(start.Add(time.Second), false)
		if a.busy() || a.nextWalk.Before(start.Add(time.Hour)) {
			t.Fatal("interrupted walk was not discarded")
		}
	}
	a := lunaIdleAnimator{random: func() float64 { return 0 }}
	a.step(now, true)
	a.step(now.Add(6*time.Second), true)
	if a.blink.IsZero() {
		t.Fatal("blink did not start")
	}
	a.step(now.Add(6300*time.Millisecond), true)
	if !a.blink.IsZero() {
		t.Fatal("blink lasts longer than 300ms")
	}
}

func TestLunaAssetsAndScanlines(t *testing.T) {
	for _, name := range []string{"skyline-idle", "skyline-dim", "walk_00", "walk_01", "walk_02", "walk_03", "walk_04", "walk_05", "walk_06", "walk_07", "sit"} {
		b, err := lunaAssets.ReadFile("assets/luna/" + name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		src, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if name != "skyline-idle" && name != "skyline-dim" {
			continue
		}
		if src.Bounds() != image.Rect(0, 0, 960, 480) {
			t.Fatal(name, src.Bounds())
		}
		raw := lunaImage(name, 960, 480, false)
		scanned := lunaImage(name, 960, 480, true)
		if scanned != lunaImage(name, 960, 480, true) {
			t.Fatal("background not cached")
		}
		for y := 0; y < 480; y++ {
			for x := 0; x < 960; x++ {
				got, want := scanned.RGBAAt(x, y), raw.RGBAAt(x, y)
				if y%2 == 1 {
					want.R = uint8(uint16(want.R) * 92 / 100)
					want.G = uint8(uint16(want.G) * 92 / 100)
					want.B = uint8(uint16(want.B) * 92 / 100)
				}
				if got != want {
					t.Fatalf("%s scanline %d,%d: %v != %v", name, x, y, got, want)
				}
			}
		}
	}
}

func TestLunaDirtyRects(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	previewFont(t)
	if err := config.Set().Screen().Skin(skinLuna); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Screen().ClockStyle(styleLuna); err != nil {
		t.Fatal(err)
	}
	if skinFilterFor(960, 480) != nil {
		t.Fatal("Luna must not stripe text with a panel filter")
	}
	applyTheme(current())
	applySkin(nil, 0, 0)
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	s := scene{now: at, phase: "idle"}
	img := image.NewRGBA(image.Rect(0, 0, 960, 480))
	r := newRenderer(img)
	r.luna.animator = lunaIdleAnimator{nextWalk: at.Add(time.Hour), walk: at.Add(-10 * time.Second), nextBlink: at.Add(time.Hour)}
	r.draw(s)
	key := r.luna.key
	s.now = s.now.Add(lunaFrame)
	r.draw(s)
	if key == "" || key != r.luna.key {
		t.Fatal("static idle cache not reused within a second")
	}
	want := image.NewRGBA(img.Rect)
	fresh := newRenderer(want)
	fresh.luna.animator = r.luna.animator
	fresh.draw(s)
	if !bytes.Equal(img.Pix, want.Pix) {
		t.Fatal("dirty sprite restore differs from a full paint")
	}
}

func TestLunaSuppressionAndTimerTouch(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	previewFont(t)
	if err := config.Set().Screen().ClockStyle(styleLuna); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Screen().TimerStyle(timerLuna); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	base := scene{now: now, phase: "idle"}
	if !lunaPlainIdle(base) {
		t.Fatal("plain daytime idle should allow animation")
	}
	for _, change := range []func(*scene){
		func(s *scene) { s.phase = "listening" }, func(s *scene) { s.showSheet = true }, func(s *scene) { s.redClock = true },
		func(s *scene) { s.nightDim = true }, func(s *scene) { s.call.Phase = phone.Ringing },
		func(s *scene) { s.ring.timer = "Tea" }, func(s *scene) { s.showDrawer = true }, func(s *scene) { s.showCamera = true },
		func(s *scene) { s.showVideo = true }, func(s *scene) { s.showCalendar = true }, func(s *scene) { s.showWeather = true },
		func(s *scene) { s.showNotification = true }, func(s *scene) { s.updateActive = true },
		func(s *scene) { s.sunrise = 0.3 }, func(s *scene) { s.announceRecording = true },
	} {
		s := base
		change(&s)
		if lunaPlainIdle(s) {
			t.Fatal("overlay permits Shiva")
		}
	}
	r := newRenderer(image.NewRGBA(image.Rect(0, 0, 960, 480)))
	s := base
	s.ring = ringState{timer: "Tea"}
	r.draw(s)
	if !r.lunaRingingDrawn {
		t.Fatal("Luna ring not drawn")
	}
	for _, p := range []image.Point{{0, 0}, {480, 80}, {864, 80}, {480, 200}, {359, 424}, {601, 424}} {
		shown, stop := r.lunaTimerTap(p)
		if !shown || stop {
			t.Fatal("unexpected stop target", p)
		}
	}
	if _, stop := r.lunaTimerTap(image.Pt(480, 424)); !stop {
		t.Fatal("stop button not hittable")
	}
	s.ring = ringState{}
	r.draw(s)
	if shown, _ := r.lunaTimerTap(image.Pt(480, 424)); shown {
		t.Fatal("stale ring touch target")
	}
	// A running countdown uses the corner; no generic timer bar gets painted.
	s.timers = []timer.Countdown{{Name: "Tea", Left: 272 * time.Second, Total: 440 * time.Second, Active: true}}
	r.draw(s)
	if timersAtFoot(s) {
		t.Fatal("legacy timer bar should be suppressed")
	}
}
