//go:build mockup && !dot && !spot

package display

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

// MOCK_OUT=/path: real renderer screenshots, with fixed spectrum/time and forced glitch.
func TestMockLunaTurns(t *testing.T) {
	lunaTurnTestSetup(t)
	previewFont(t)
	if err := config.Set().Screen().Language("de"); err != nil {
		t.Fatal(err)
	}
	out := os.Getenv("MOCK_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 11, 3, 21, 7, 0, 0, time.Local)
	long := "Morgen wird es in Hamburg wechselhaft: Vormittags dichte Wolken und um 10 Uhr leichter Regen, nachmittags lockert es auf und die Sonne kommt zwischendurch heraus. Die Höchstwerte liegen bei 14 Grad, nachts kühlt es auf 6 Grad ab. Der Wind kommt mäßig aus Südwest mit Böen bis 45 km/h, an der Elbe auch etwas mehr. Für deinen Termin um 15:30 Uhr beim Zahnarzt nimmst du am besten einen Schirm mit, falls sich der Regen bis Mittag hält. Am Abend zum Training um 18 Uhr sollte es trocken sein."
	for _, name := range []string{"02-listening", "03-thinking-glitch", "04-speaking-long", "04b-speaking-last", "05-followup", "05b-followup-last-page", "08-night-wake-optional", "09-lingering"} {
		s := lunaTestTurn(at, "listening")
		switch name {
		case "03-thinking-glitch":
			s.phase = "thinking"
			s.activity = [2]voice.ToolActivity{{Icon: "weather-partly-cloudy", Label: "Wetter Hamburg", Status: "ok"}, {Icon: "calendar", Label: "Termine morgen", Status: "running"}}
		case "04-speaking-long", "04b-speaking-last":
			s.phase = "replying"
			s.reply = long
			s.timers = []timer.Countdown{{Name: "Nudeln", Left: 272 * time.Second, Total: 439 * time.Second, Active: true}}
			s.activity = [2]voice.ToolActivity{{Icon: "weather-partly-cloudy", Label: "Wetter Hamburg", Status: "ok"}, {Icon: "calendar", Label: "Termine morgen", Status: "ok"}}
			if name == "04b-speaking-last" {
				s.said = 1
			}
		case "05-followup", "05b-followup-last-page":
			s.followUp = true
			s.followUpStarted = at.Add(-4200 * time.Millisecond)
			s.followUpUntil = at.Add(5800 * time.Millisecond)
			s.heard = "Stell einen Timer für die Nudeln"
			s.reply = "Klar. Wie lange sollen die Nudeln kochen – acht oder zehn Minuten?"
			if name == "05b-followup-last-page" {
				s.reply = long
			}
		case "08-night-wake-optional":
			s.eq.night = true
		case "09-lingering":
			s.phase = "lingering"
			s.reply = long
		}
		s.eq.phase = s.phase
		img := image.NewRGBA(image.Rect(0, 0, 960, 480))
		r := newRenderer(img)
		r.lunaTurnCache.phase = s.phase
		r.lunaTurnCache.entered = at.Add(-time.Second)
		r.lunaTurnCache.nextGlitch = at.Add(time.Hour)
		if name == "03-thinking-glitch" {
			r.lunaTurnCache.glitchUntil = at.Add(100 * time.Millisecond)
		}
		r.draw(s)
		mockSave(t, out, name, img)
	}
}
