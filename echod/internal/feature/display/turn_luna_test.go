//go:build !dot && !spot

package display

import (
	"bytes"
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

func lunaTurnTestSetup(t *testing.T) {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	for _, err := range []error{config.Set().Screen().Skin(skinLuna), config.Set().Screen().TurnStyle("luna"), config.Set().Screen().TimerStyle(timerLuna)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	applyTheme(current())
	applySkin(nil, 0, 0)
}

func lunaTestTurn(at time.Time, phase string) scene {
	levels := make([]float64, 32)
	for i := range levels {
		levels[i] = 0.7
	}
	return scene{now: at, since: at.Add(-time.Second), phase: phase, heard: "Wie wird das Wetter morgen in Hamburg?", eq: &eqView{style: "luna", phase: phase, level: levels}}
}

func TestLunaFollowUpAndCadence(t *testing.T) {
	at := time.Unix(1000, 0)
	s := lunaTestTurn(at, "listening")
	s.followUp = true
	if lunaFollowUp(s) {
		t.Fatal("missing timing must fall back to listening")
	}
	s.followUpStarted, s.followUpUntil = at.Add(-4*time.Second), at.Add(6*time.Second)
	if !lunaFollowUp(s) || lunaFollowFraction(s) != 0.6 {
		t.Fatalf("fraction = %v", lunaFollowFraction(s))
	}
	s.now = s.followUpUntil
	if lunaFollowUp(s) || lunaFollowFraction(s) != 0 {
		t.Fatal("expired countdown")
	}
	s.now = at
	for _, phase := range []string{"listening", "thinking", "replying"} {
		s.phase = phase
		if lunaTurnFrame(s) != 100*time.Millisecond {
			t.Fatal(phase)
		}
	}
	s.phase = "lingering"
	s.since = at.Add(-200 * time.Millisecond)
	if lunaTurnFrame(s) != 100*time.Millisecond {
		t.Fatal("blink must finish at 10FPS")
	}
	s.since = at.Add(-300 * time.Millisecond)
	if lunaTurnFrame(s) != time.Second {
		t.Fatal("lingering must settle at 1FPS regardless of speaker tail")
	}
	s.phase = "thinking"
	if lunaLevel(s) != 0 {
		t.Fatal("thinking must not use eqFor's synthetic voice")
	}
	s.phase = "lingering"
	if lunaLevel(s) != 0 {
		t.Fatal("lingering scope must be flat")
	}
}

func TestLunaTurnTouchesOnlyLastDrawnPage(t *testing.T) {
	lunaTurnTestSetup(t)
	r := newRenderer(image.NewRGBA(image.Rect(0, 0, 960, 480)))
	at := time.Unix(1000, 0)
	for _, tc := range []struct {
		phase      string
		eye, right string
	}{{"listening", "stop", ""}, {"thinking", "stop", ""}, {"replying", "listen", "stop"}, {"lingering", "", ""}} {
		s := lunaTestTurn(at, tc.phase)
		r.draw(s)
		if got := r.lunaTurnTap(image.Pt(124, 190)); got != tc.eye {
			t.Errorf("%s eyes = %q", tc.phase, got)
		}
		if got := r.lunaTurnTap(image.Pt(500, 200)); got != tc.right {
			t.Errorf("%s text = %q", tc.phase, got)
		}
		for _, p := range []image.Point{{124, 119}, {500, 64}, {20, 200}, {920, 200}} {
			if got := r.lunaTurnTap(p); got != "" {
				t.Errorf("unexpected hit at %v", p)
			}
		}
	}
	s := lunaTestTurn(at, "replying")
	r.draw(s)
	s.showSheet = true
	r.draw(s)
	if r.lunaTurnTap(image.Pt(124, 190)) != "" {
		t.Fatal("covered turn retained its hitbox")
	}
	s.showSheet = false
	s.eq.style = "face"
	r.draw(s)
	if r.lunaTurnTap(image.Pt(124, 190)) != "" {
		t.Fatal("another style retained Luna hitbox")
	}
}

func TestLunaGlitchStaysLeftAndToolsAreOptional(t *testing.T) {
	lunaTurnTestSetup(t)
	at := time.Unix(1000, 0)
	s := lunaTestTurn(at, "thinking")
	render := func(glitch bool, activity [2]voice.ToolActivity) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 960, 480))
		r := newRenderer(img)
		r.lunaTurnCache.phase = s.phase
		r.lunaTurnCache.nextGlitch = at.Add(time.Hour)
		if glitch {
			r.lunaTurnCache.glitchUntil = at.Add(100 * time.Millisecond)
		}
		sc := s
		sc.activity = activity
		r.draw(sc)
		return img
	}
	blank := [2]voice.ToolActivity{}
	plain, glitch := render(false, blank), render(true, blank)
	same := func(a, b *image.RGBA, box image.Rectangle) bool {
		for y := box.Min.Y; y < box.Max.Y; y++ {
			x0, x1 := a.PixOffset(box.Min.X, y), a.PixOffset(box.Max.X, y)
			if !bytes.Equal(a.Pix[x0:x1], b.Pix[x0:x1]) {
				return false
			}
		}
		return true
	}
	if !same(plain, glitch, image.Rect(344, 0, 960, 480)) {
		t.Fatal("glitch changed reply column")
	}
	if same(plain, glitch, image.Rect(40, 120, 320, 260)) {
		t.Fatal("glitch produced no eye displacement")
	}
	unused := [2]voice.ToolActivity{{Label: "must not show", Status: ""}, {Label: "must not show", Status: "unknown"}}
	if !same(plain, render(false, unused), image.Rect(40, 372, 320, 428)) {
		t.Fatal("unused/unknown tool activity was drawn")
	}
	active := [2]voice.ToolActivity{{Icon: "weather-partly-cloudy", Label: "Wetter Hamburg", Status: "ok"}, {Icon: "calendar", Label: "Termine morgen", Status: "running"}}
	if same(plain, render(false, active), image.Rect(40, 372, 320, 428)) {
		t.Fatal("tool status missing")
	}
}

func TestLunaTurnNightAndEyeCache(t *testing.T) {
	lunaTurnTestSetup(t)
	img := image.NewRGBA(image.Rect(0, 0, 960, 480))
	r := newRenderer(img)
	s := lunaTestTurn(time.Unix(1000, 0), "replying")
	s.reply = "Das Licht ist aus."
	s.eq.night = true
	r.draw(s)
	for y := 0; y < 480; y++ {
		for x := 0; x < 960; x++ {
			c := img.RGBAAt(x, y)
			if c.G > c.R || c.B > c.R {
				t.Fatalf("night has nonred asset/pixel at %d,%d: %v", x, y, c)
			}
		}
	}
	if a, b := r.lunaEyes(48, 100, 0, lunaCyan, false), r.lunaEyes(48, 100, 0, lunaCyan, false); a != b {
		t.Fatal("eye buffer not cached")
	}
}
