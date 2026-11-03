//go:build !dot && !spot

package display

import (
	"fmt"
	"image"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/lib/locale"
)

func (r *renderer) lunaClock(s scene) {
	// A clock style works independently of the skin, including its own backdrop.
	if skin() != skinLuna {
		r.lunaBackdrop(s)
	}
	face := r.lunaFace(true, 150)
	digits := clockHM(s.now)
	// Fixed digit cells keep narrow ones from making the clock jump each minute.
	cell := r.width(face, "0")
	x := r.s(56)
	for _, ch := range digits {
		text := string(ch)
		c := lunaText
		w := cell
		if ch == ':' {
			c = lunaCyan
			w = r.width(face, ":")
			x += r.s(6)
		}
		r.text(face, text, x+(w-r.width(face, text))/2, r.s(264), c)
		x += w
		if ch == ':' {
			x += r.s(6)
		}
	}
	dateFace := r.lunaFace(false, 28)
	date := locale.LongDate(s.now, screenLang())
	first := string([]rune(date)[0])
	dx := inkOf(face, string([]rune(digits)[0])).Min.X - inkOf(dateFace, first).Min.X
	r.lunaMid(dateFace, date, 56+dx*960/r.w, 304, lunaDim)
	if suffix := clockSuffix(s.now); suffix != "" {
		r.lunaMid(r.lunaFace(false, 22), suffix, 480, 250, lunaDim)
	}
	r.lunaWeather(s)
	r.lunaEvents(s)
	// Mini eyes are drawn last, outside the static idle cache, when eligible.
	if !lunaPlainIdle(s) {
		r.lunaMiniEyes(false)
	}
	r.alertBadge(s)
}

func (r *renderer) lunaWeather(s scene) {
	if s.weather.Temp == "" && s.weather.Condition == "" {
		return
	}
	glyph, _ := weatherGlyph(s.weather.Condition)
	r.mdiAt(glyph, r.s(150), r.s(66), r.s(28), lunaCyan)
	label := strings.TrimSpace(s.weather.Temp + " · " + conditionWords(s.weather.Condition))
	face := r.lunaFace(false, 24)
	r.lunaMid(face, r.fit(face, label, r.s(430)), 176, 66, lunaText)
	if len(s.style.days) > 0 {
		day := s.style.days[0]
		x := 176 + r.width(face, label)*960/r.w + 14
		if span := fmt.Sprintf("↑ %.0f°  ↓ %.0f°", day.High, day.Low); x+r.width(face, span)*960/r.w < 800 {
			r.lunaMid(face, span, x, 66, lunaDim) // clear of the timer ring's scale
		}
	}
}

func (r *renderer) lunaEvents(s scene) {
	// No calendar data means no column, not a clock centered over the tower.
	if len(s.style.next) == 0 {
		return
	}
	r.setNextAt(image.Rect(r.s(576), r.s(120), r.s(896), r.s(336)))
	today := s.now.Format("2006-01-02")
	tomorrow := s.now.AddDate(0, 0, 1).Format("2006-01-02")
	group, row, prev := -1, 0, ""
	for _, e := range s.style.next {
		day := e.Start.In(s.now.Location()).Format("2006-01-02")
		if day != prev {
			group++
			if group > 1 {
				break
			}
			row, prev = 0, day
			label := locale.ShortDate(e.Start, screenLang())
			if day == today {
				label = strings.ToUpper(tr("today"))
			} else if day == tomorrow {
				label = strings.ToUpper(tr("tomorrow"))
			}
			r.lunaMid(r.lunaFace(true, 15), label, 600, 128+group*120, lunaDim)
		}
		if row >= 2 {
			continue
		}
		y := 160 + group*120 + row*40
		label := clockText(e.Start)
		if e.AllDay {
			label = tr("luna_all_day") // short enough for the time column
		}
		timeFace := r.lunaFace(false, 22)
		r.lunaMid(timeFace, r.fit(timeFace, label, r.s(64)), 600, y, lunaDim)
		face := r.lunaFace(false, 24)
		r.lunaMid(face, r.fit(face, e.Summary, r.s(224)), 672, y, lunaText)
		row++
	}
}
