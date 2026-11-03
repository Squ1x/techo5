//go:build !dot && !spot

package display

import (
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
)

func lunaTimerRinging(s scene) bool {
	return timerStyle() == timerLuna && s.ring.timer != "" && s.ring.alarm == nil && !s.ring.preview && !s.ring.silenced && !s.redClockFace() && !nightNow(s.now)
}

// lunaTimerCorner is also used by the turn renderer (small=true).
func (r *renderer) lunaTimerCorner(s scene, small bool) {
	if timerStyle() != timerLuna {
		return
	}
	run := runningTimers(s)
	if len(run) == 0 {
		return
	}
	cx, cy, rad, px := 864, 80, 32, 13
	if small {
		cx, cy, rad, px = 872, 76, 24, 11
	}
	r.lunaRingScale(cx, cy, rad, 1)
	for i, t := range run[:min(3, len(run))] {
		c := lunaCyan
		if t.Left < 10*time.Second {
			c = lunaMagenta
		}
		radius := rad - i*6
		r.lunaGlowArc(r.sf(cx), r.sf(cy), r.sf(radius), r.sf(4), 0, 360*timerFrac(t), c)
	}
	label := timer.LeftText(run[0].Left)
	if len(label) == 4 {
		label = "0" + label
	}
	f := r.lunaMono(px)
	for px > 8 && r.width(f, label) > r.s((rad-5)*2) {
		px--
		f = r.lunaMono(px)
	}
	r.lunaMid(f, label, cx-r.width(f, label)*960/r.w/2, cy, lunaText)
}

// Scale marks are cached separately from the second-by-second remaining arc.
func (r *renderer) lunaRingScale(cx, cy, rad int, scale float64) {
	key := [4]int{r.s(rad), int(scale * 100), r.s(cx), r.s(cy)}
	if r.luna.scales == nil {
		r.luna.scales = make(map[[4]int]*image.RGBA)
	}
	img := r.luna.scales[key]
	if img == nil {
		pad := r.s(rad) + int(float64(r.s(8))*scale) + r.s(3)
		img = image.NewRGBA(image.Rect(0, 0, 2*pad+1, 2*pad+1))
		dst := r.dst
		r.dst = img
		for i := 0; i < 60; i++ {
			length := 2.0 * scale
			if i%5 == 0 {
				length = 5 * scale
			}
			a := float64(i)*math.Pi/30 - math.Pi/2
			inner := r.sf(rad) + 3*r.sk()
			outer := inner + length*r.sk()
			r.stroke(pad+int(math.Cos(a)*inner), pad+int(math.Sin(a)*inner), pad+int(math.Cos(a)*outer), pad+int(math.Sin(a)*outer), max(1, r.s(1)), lunaRules)
		}
		r.arcBand(float64(pad), float64(pad), r.sf(rad), r.sf(1), 0, 360, faded(lunaRules, 153))
		r.dst = dst
		r.luna.scales[key] = img
	}
	at := image.Pt(r.s(cx)-img.Rect.Dx()/2, r.s(cy)-img.Rect.Dy()/2)
	draw.Draw(r.dst, img.Rect.Add(at), img, image.Point{}, draw.Over)
}

func (r *renderer) lunaRinging(s scene) {
	r.lunaSkyline(true)
	titleFace := r.lunaFace(false, 28)
	title := r.fit(titleFace, s.ring.timer, r.s(720))
	r.lunaMid(titleFace, title, 480-r.width(titleFace, title)*960/r.w/2, 52, lunaText)
	c := lunaMagenta
	if s.now.UnixMilli()%1600 >= 800 {
		c = lunaAmber
	}
	r.lunaRingScale(480, 224, 128, 2.5)
	pulse := 0.65 + 0.35*(1+math.Sin(float64(s.now.UnixMilli())*math.Pi/800))/2
	r.lunaGlowArc(r.sf(480), r.sf(224), r.sf(128), r.sf(8), 0, 360, faded(c, uint8(255*pulse)))
	f := r.lunaMono(64)
	r.lunaMid(f, "00:00", 480-r.width(f, "00:00")*960/r.w/2, 226, lunaText)
	b := image.Rect(r.s(360), r.s(384), r.s(600), r.s(464))
	r.roundRect(b, r.s(16), faded(lunaMagenta, 31))
	r.roundStroke(b, r.sf(16), r.sf(3), lunaMagenta)
	r.mdiAt("stop", r.s(432), r.s(424), r.s(32), lunaMagenta)
	stopFace := r.lunaFace(true, 32)
	r.lunaMid(stopFace, tr("stop"), 500-r.width(stopFace, tr("stop"))*960/r.w/2, 424, lunaText)
	for _, cx := range []int{70, 106} {
		r.lunaOval(cx, 66, 16, 21, faded(lunaCyan, 26))
		r.lunaOval(cx, 66, 12, 17, faded(lunaCyan, 64))
		r.lunaOval(cx, 66, 9, 14, lunaCyan)
	}
	// This is a system timer view; touch dispatch is published only for this frame.
	r.zmu.Lock()
	r.lunaStopAt = image.Rect(r.s(360), r.s(376), r.s(600), r.h)
	r.lunaRingingDrawn = true
	r.zmu.Unlock()
}

func (r *renderer) lunaTimerTap(at image.Point) (shown, stop bool) {
	r.zmu.Lock()
	defer r.zmu.Unlock()
	return r.lunaRingingDrawn, at.In(r.lunaStopAt)
}
