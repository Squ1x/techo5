//go:build !dot && !spot

package display

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"golang.org/x/image/vector"
)

// Only the small eye layer is cached; no blur or full-frame animation buffers.
type lunaTurnPaint struct {
	eyes                             map[[3]int]*image.RGBA
	phase                            string
	entered, nextGlitch, glitchUntil time.Time
}

func lunaTurnFrame(s scene) time.Duration {
	if s.phase == "lingering" && s.now.Sub(s.since) >= 300*time.Millisecond {
		return time.Second
	}
	return 100 * time.Millisecond
}

func lunaFollowUp(s scene) bool {
	return s.phase == "listening" && s.followUp && !s.followUpStarted.IsZero() && s.followUpUntil.After(s.followUpStarted) && s.now.Before(s.followUpUntil)
}

func lunaFollowFraction(s scene) float64 {
	if !lunaFollowUp(s) {
		return 0
	}
	return min(1, max(0, float64(s.followUpUntil.Sub(s.now))/float64(s.followUpUntil.Sub(s.followUpStarted))))
}

func (r *renderer) lunaTurnTap(at image.Point) string {
	r.zmu.Lock()
	phase := r.lunaTurnDrawn
	r.zmu.Unlock()
	if at.Y < r.s(120) {
		return ""
	}
	if at.In(image.Rect(r.s(40), r.s(120), r.s(320), r.s(320))) {
		switch phase {
		case "replying":
			return "listen"
		case "listening", "thinking":
			return "stop"
		}
	}
	if phase == "replying" && at.In(image.Rect(r.s(344), r.s(120), r.s(896), r.s(440))) {
		return "stop"
	}
	return ""
}

func (r *renderer) lunaTurn(s scene) {
	night := s.eq != nil && s.eq.night
	accent, text, subdued := lunaCyan, lunaText, lunaDim
	if night {
		accent, text, subdued = color.RGBA{0x9a, 0x10, 0x10, 255}, color.RGBA{0xb0, 0x15, 0x15, 255}, color.RGBA{0x7a, 0x0a, 0x0a, 255}
		draw.Draw(r.dst, r.dst.Bounds(), image.Black, image.Point{}, draw.Src)
	} else {
		r.lunaSkyline(true)
	}
	p := &r.lunaTurnCache
	if p.phase != s.phase {
		p.phase, p.entered = s.phase, s.now
		p.nextGlitch = s.now.Add(time.Duration(1200+rand.IntN(1301)) * time.Millisecond)
		p.glitchUntil = time.Time{}
	}
	follow := lunaFollowUp(s)
	height, scale, tilt := 48, 100, 0
	dy := 0
	switch s.phase {
	case "listening":
		if follow {
			tilt = 12
		} else {
			scale = 100 + min(2, max(0, int(s.now.Sub(p.entered)/(100*time.Millisecond))))*4
		}
	case "thinking":
		height = 18
	case "replying":
		dy = int(math.Round(2 * math.Sin(float64(s.now.UnixMilli()%1200)*2*math.Pi/1200) * lunaLevel(s)))
	case "lingering":
		height = 24
		if elapsed := s.now.Sub(s.since); elapsed < 300*time.Millisecond && elapsed >= 100*time.Millisecond {
			height = 3
		}
	}
	eyes := r.lunaEyes(height, scale, tilt, accent, night)
	if s.phase == "thinking" && !night {
		if !s.now.Before(p.nextGlitch) {
			p.glitchUntil = s.now.Add(200 * time.Millisecond)
			p.nextGlitch = s.now.Add(time.Duration(1200+rand.IntN(1301)) * time.Millisecond)
		}
		if s.now.Before(p.glitchUntil) {
			r.lunaGlitch(eyes)
		}
	}
	draw.Draw(r.dst, eyes.Bounds().Add(image.Pt(0, r.s(dy))), eyes, eyes.Bounds().Min, draw.Over)
	if s.phase == "thinking" && s.now.Before(p.glitchUntil) && !night {
		// Copy just two strips of the cached eye layer, never the text column.
		for _, strip := range []struct{ y, h, dx int }{{182, 6, 10}, {200, 4, -8}} {
			b := image.Rect(r.s(40), r.s(strip.y), r.s(320), r.s(strip.y+strip.h))
			offset := image.Pt(r.s(strip.dx), 0)
			clipped := b.Add(offset).Intersect(image.Rect(r.s(40), r.s(120), r.s(320), r.s(260)))
			draw.Draw(r.dst, clipped, eyes, clipped.Min.Sub(offset), draw.Over)
		}
	}
	r.lunaScope(s, accent, night)
	if !night {
		r.lunaTools(s)
	}
	if s.heard != "" {
		x, right := 344, 896
		if s.phase == "listening" && !follow {
			pulse := uint8(160 + 95*(0.5+0.5*math.Sin(float64(s.now.UnixMilli()%1600)*2*math.Pi/1600)))
			r.mdiIcon("microphone", r.s(x), r.s(52), 24, faded(accent, pulse))
			x += 32
		}
		if len(runningTimers(s)) > 0 && timerStyle() == timerLuna {
			right = 824
		}
		f := r.lunaFace(false, 22)
		r.lunaMid(f, r.clipTo(f, fmt.Sprintf(tr("luna_you_said"), s.heard), r.s(right-x)), x, 64, subdued)
	}
	bottom := r.lunaReply(s, follow, text)
	if follow && !night {
		r.lunaFollowFrame(s)
		f := r.lunaFace(false, 22)
		label := tr("luna_listening")
		y := min(408, bottom+32)
		w := r.width(f, label) + r.s(58)
		r.roundStroke(image.Rect(r.s(344), r.s(y-20), r.s(344)+w, r.s(y+20)), r.sf(20), r.sf(2), lunaMagenta)
		r.mdiIcon("microphone", r.s(356), r.s(y-11), 22, lunaMagenta)
		r.lunaMid(f, label, 384, y, lunaText)
	}
	if !night {
		r.lunaTimerCorner(s, true)
	}
	r.zmu.Lock()
	r.lunaTurnDrawn = s.phase
	r.zmu.Unlock()
}

func lunaLevel(s scene) float64 {
	if s.eq == nil || len(s.eq.level) == 0 || s.phase == "thinking" || s.phase == "lingering" {
		return 0
	}
	sum := 0.0
	for _, v := range s.eq.level {
		if !math.IsNaN(v) {
			sum += min(1, max(0, v))
		}
	}
	return sum / float64(len(s.eq.level))
}

func (r *renderer) lunaScope(s scene, c color.RGBA, night bool) {
	if !night {
		r.roundStroke(image.Rect(r.s(72), r.s(300), r.s(288), r.s(348)), 0, r.sf(1), lunaRules)
		washOver(r.dst, image.Rect(r.s(72), r.s(324), r.s(288), r.s(324)+max(1, r.s(1))), faded(lunaRules, 153))
		for _, x := range []int{126, 180, 234} {
			r.stroke(r.s(x), r.s(319), r.s(x), r.s(329), max(1, r.s(1)), lunaRules)
		}
	}
	amp := lunaLevel(s)
	t := float64(s.now.UnixMilli()%12000) / 1000
	pts := make([][2]float64, 55)
	for i := range pts {
		u := float64(i) / 54
		v := (math.Sin(2*math.Pi*2.2*u+t) + math.Sin(2*math.Pi*4.7*u+2*t)/2 + math.Sin(2*math.Pi*7.9*u+3*t)/3) / 1.6
		pts[i] = [2]float64{float64(72 + i*4), 324 - 20*amp*math.Pow(math.Sin(math.Pi*u), 1.5)*v}
	}
	for _, pass := range []struct {
		w float64
		a uint8
	}{{10, 26}, {6, 64}, {2, 255}} {
		if night && pass.a != 255 {
			continue
		}
		r.lunaTrace(pts, pass.w, faded(c, pass.a))
	}
	if s.phase == "thinking" {
		x := 72 + int(216*float64(s.now.UnixMilli()%1200)/1200)
		if !night {
			r.lunaOval(x, 324, 11, 11, faded(c, 26))
			r.lunaOval(x, 324, 7, 7, faded(c, 64))
		}
		r.lunaOval(x, 324, 3, 3, c)
	}
}

// Eye variants have only discrete wake/blink sizes; the cache stays bounded.
func (r *renderer) lunaEyes(height, scale, tilt int, c color.RGBA, night bool) *image.RGBA {
	key := [3]int{height, scale, tilt}
	if night {
		key[2] += 100
	}
	p := &r.lunaTurnCache
	if p.eyes == nil {
		p.eyes = make(map[[3]int]*image.RGBA)
	}
	if img := p.eyes[key]; img != nil {
		return img
	}
	dst := r.dst
	// The layer holds every eye's outer halo, tilted and raised or grown by 8 %, so no shape box leaves it.
	box := image.Rect(r.s(40), r.s(104), r.s(320), r.s(276))
	r.dst = image.NewRGBA(box)
	for i, cx := range []int{124, 236} {
		cy := 190
		if tilt != 0 && i == 1 {
			cy -= 6
		}
		if height == 18 {
			cy += 6 // squinting lids sit a little lower, as in the reference
		}
		rx, ry := 32*float64(scale)/100, float64(height)*float64(scale)/100
		if height == 18 {
			ry = 24
		}
		eye := func(grow float64, col color.RGBA, core bool) {
			a, b, y := rx+grow, ry+grow, float64(cy)
			if core {
				a, b, y = rx/2, ry/2, y-ry*0.12
			}
			box := image.Rect(r.s(cx-72), r.s(cy-72), r.s(cx+72), r.s(cy+72)).Intersect(r.dst.Bounds())
			r.fillShape(box, col, func(z *vector.Rasterizer, w, h float32) {
				angle := float64(tilt) * math.Pi / 180
				for j := 0; j < 72; j++ {
					t := 2 * math.Pi * float64(j) / 72
					x, ey := a*math.Cos(t), b*math.Sin(t)
					if height == 18 {
						ey = max(ey, -10-grow)
					}
					px := float32(((float64(cx) + x*math.Cos(angle) - ey*math.Sin(angle)) * r.sk())) - float32(box.Min.X)
					py := float32(((y + x*math.Sin(angle) + ey*math.Cos(angle)) * r.sk())) - float32(box.Min.Y)
					if j == 0 {
						z.MoveTo(px, py)
					} else {
						z.LineTo(px, py)
					}
				}
				z.ClosePath()
			})
		}
		if !night {
			eye(14, faded(c, 26), false)
			eye(6, faded(c, 64), false)
		}
		eye(0, c, false)
		if height >= 24 && !night {
			eye(0, faded(mix(c, color.RGBA{255, 255, 255, 255}, 0.45), 140), true)
		}
	}
	img := r.dst
	r.dst = dst
	p.eyes[key] = img
	return img
}

func (r *renderer) lunaGlitch(eyes *image.RGBA) {
	for _, layer := range []struct {
		dx int
		c  color.RGBA
	}{{-4, lunaCyan}, {4, lunaMagenta}} {
		draw.DrawMask(r.dst, eyes.Bounds().Add(image.Pt(r.s(layer.dx), 0)), image.NewUniform(faded(layer.c, 128)), image.Point{}, eyes, eyes.Bounds().Min, draw.Over)
	}
}

func (r *renderer) lunaReply(s scene, follow bool, c color.RGBA) int {
	if s.reply == "" || (!follow && s.phase != "replying" && s.phase != "lingering") {
		return 96
	}
	limit := 436
	if follow {
		limit = 356
	}
	size := 30
	lines := r.wrap(r.lunaFace(false, size), s.reply, r.s(552))
	// Prefer the reference's 26px page size for long speech; 24px is the lower bound.
	if len(lines)*39 > limit-96 {
		size = 26
	}
	if count := len(r.wrap(r.lunaFace(false, size), s.reply, r.s(552))); count*34 > 2*(limit-96) || follow && count*34 > limit-96 {
		size = 24
	}
	f := r.lunaFace(false, size)
	pitch := int(math.Round(float64(size) * 1.3))
	fit := max(1, (limit-96)/pitch)
	lines = r.wrap(f, s.reply, r.s(552))
	if len(lines) > fit {
		lines = r.wrap(f, s.reply, r.s(552)-r.width(f, " …"))
	}
	said := s.said
	if follow || s.phase == "lingering" {
		said = 1
	}
	page := replyPage(lines, fit, said)
	top := 96
	for _, line := range page {
		ink := inkOf(f, line)
		r.text(f, line, r.s(344), r.s(top)-ink.Min.Y, c)
		top += pitch
	}
	pages := (len(lines) + fit - 1) / fit
	if pages > 1 && !follow { // a follow-up's countdown runs where the dots would be
		// replyPage is the sole paging authority, including its character-weighted boundary rule.
		current := 0
		for i := 0; i < len(lines); i += fit {
			if strings.TrimSuffix(page[0], " …") == lines[i] {
				current = i / fit
				break
			}
		}
		// Bound the indicators to the column even for very long replies.
		start := max(0, current-16)
		end := min(pages, start+33)
		for i := start; i < end; i++ {
			col := lunaRules
			if i == current {
				col = lunaCyan
			}
			if s.eq != nil && s.eq.night {
				col = faded(c, 64)
				if i == current {
					col = c
				}
			}
			r.lunaOval(892-(end-1-i)*16, 456, 4, 4, col)
		}
	}
	return top - pitch + size
}

func (r *renderer) lunaFollowFrame(s scene) {
	alpha := uint8(255 * (0.65 + 0.25*math.Sin(float64(s.now.UnixMilli()%1600)*2*math.Pi/1600)))
	c := faded(lunaMagenta, alpha)
	// Four thin strips and four local corner arcs; never scan the frame's interior.
	for _, edge := range []image.Rectangle{image.Rect(24, 7, 936, 10), image.Rect(24, 470, 936, 473), image.Rect(7, 24, 10, 456), image.Rect(950, 24, 953, 456)} {
		washOver(r.dst, image.Rect(r.s(edge.Min.X), r.s(edge.Min.Y), r.s(edge.Max.X), r.s(edge.Max.Y)), c)
	}
	for _, corner := range [][3]int{{24, 24, 270}, {936, 24, 0}, {936, 456, 90}, {24, 456, 180}} {
		r.arcBand(r.sf(corner[0]), r.sf(corner[1]), r.sf(31)/2, r.sf(3), float64(corner[2]), float64(corner[2]+90), c)
	}
	x := 24 + int(912*lunaFollowFraction(s))
	if x > 24 {
		washOver(r.dst, image.Rect(r.s(24), r.s(455), r.s(x), r.s(458)), lunaMagenta)
	}
}

func (r *renderer) lunaTools(s scene) {
	f := r.lunaMono(18)
	for i, item := range s.activity {
		status, col := "", lunaDim
		switch item.Status {
		case "running":
			status = strings.Repeat(".", 1+int(s.now.UnixMilli()/300%3))
		case "ok":
			status, col = "ok", lunaCyan
		case "missing":
			status, col = tr("luna_tool_missing"), lunaMagenta
		case "error":
			status, col = tr("luna_tool_error"), lunaMagenta
		default:
			continue
		}
		y := 384 + i*28
		right := r.s(320) - r.width(f, status)
		r.lunaMid(f, ">", 40, y, lunaDim)
		r.mdiIcon(item.Icon, r.s(58), r.s(y-9), 18, lunaCyan)
		r.lunaMid(f, r.clipTo(f, item.Label, max(0, right-r.s(90))), 82, y, lunaDim)
		r.text(f, status, right, r.s(y)-(inkOf(f, status).Min.Y+inkOf(f, status).Max.Y)/2, col)
	}
}
