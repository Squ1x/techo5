//go:build !dot && !spot

package display

import (
	"fmt"
	"image"
	"image/draw"
	"math/rand/v2"
	"time"

	"golang.org/x/image/font"

	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
)

const lunaFrame = 100 * time.Millisecond

func lunaAnimationActive(r *renderer) bool           { return r.luna.animator.busy() || r.lunaRingingDrawn }
func lunaDisplayOff(r *renderer, now time.Time)      { r.luna.animator.reset(now); r.luna.key = "" }
func lunaStopTap(r *renderer, x, y int) (bool, bool) { return r.lunaTimerTap(image.Pt(x, y)) }

// No goroutine or wall clock in the animator: time and randomness are injectable.
type lunaIdleAnimator struct {
	nextWalk, walk, nextBlink, blink time.Time
	random                           func() float64
}

func (a *lunaIdleAnimator) jitter() float64 {
	if a.random != nil {
		return max(0, min(1, a.random()))
	}
	return rand.Float64()
}
func (a *lunaIdleAnimator) later(now time.Time) {
	a.nextWalk = now.Add(time.Hour + time.Duration(a.jitter()*float64(time.Hour)))
}
func (a *lunaIdleAnimator) reset(now time.Time) {
	a.walk, a.blink = time.Time{}, time.Time{}
	a.later(now)
	a.nextBlink = now.Add(6*time.Second + time.Duration(a.jitter()*float64(6*time.Second)))
}
func (a *lunaIdleAnimator) step(now time.Time, allowed bool) {
	if !allowed {
		a.reset(now)
		return
	}
	if a.nextWalk.IsZero() {
		a.reset(now)
	}
	if !a.walk.IsZero() && now.Sub(a.walk) >= 257*lunaFrame {
		a.walk = time.Time{}
		a.later(now)
	}
	if a.walk.IsZero() && !now.Before(a.nextWalk) {
		a.walk = now
	}
	if !a.blink.IsZero() && now.Sub(a.blink) >= 300*time.Millisecond {
		a.blink = time.Time{}
		a.nextBlink = now.Add(6*time.Second + time.Duration(a.jitter()*float64(6*time.Second)))
	}
	if a.blink.IsZero() && !now.Before(a.nextBlink) {
		a.blink = now
	}
}

// summon starts a walk on the next step unless one is under way (a tap on the sign).
func (a *lunaIdleAnimator) summon(now time.Time) {
	if a.walk.IsZero() {
		a.nextWalk = now
	}
}
func (a *lunaIdleAnimator) busy() bool { return !a.walk.IsZero() || !a.blink.IsZero() }
func (a *lunaIdleAnimator) sprite(now time.Time) (x, frame int, ok bool) {
	if a.walk.IsZero() {
		return 0, 0, false
	}
	tick := int(now.Sub(a.walk) / lunaFrame)
	if tick < 0 || tick >= 257 {
		return 0, 0, false
	}
	return -130 + int(float64(tick)*4.25), tick % 8, true
}

type lunaIdlePaint struct {
	animator   lunaIdleAnimator
	faces      map[lunaFaceKey]font.Face
	scales     map[[4]int]*image.RGBA
	base       *image.RGBA
	key        string
	lastSprite image.Rectangle
	// signLive is whether the last frame was the plain idle clock, and walkAsked a tap on the sign
	// for the next one; both under zmu, as the tap comes from the touch goroutine.
	signLive, walkAsked bool
}

// lunaSignTap asks for a walk when the tap is on the SHIVA sign (skyline asset, box 436–462 ×
// 318–424) of a plain idle clock; the area is widened for a finger. Reports whether it took the tap.
func (r *renderer) lunaSignTap(at image.Point) bool {
	r.zmu.Lock()
	defer r.zmu.Unlock()
	if !r.luna.signLive || !at.In(image.Rect(r.s(420), r.s(306), r.s(480), r.s(436))) {
		return false
	}
	r.luna.walkAsked = true
	return true
}

// Never let an overlay hide a still-running dog. A countdown may coexist
// (spec §7a / mock 01b); ringing may not. Off resets before frame's early return.
func lunaPlainIdle(s scene) bool {
	return s.style.style() == styleLuna && s.phase == "idle" && !s.redClock && !s.nightDim && !nightNow(s.now) &&
		!s.ring.any() && s.call.Phase == phone.Idle && !s.showSheet && !s.showDrawer &&
		!s.showDash && !s.showCamera && !s.showVideo && !s.showVideoAsk && !s.showCalendar && !s.showDeck && !s.showWeather &&
		!s.showRadar && !s.showAlert && !s.nowPlaying && !s.showVolume && !s.showNotification && !s.showAnnouncement &&
		!s.showReminder && !s.showWifi && !s.bt.Pairing && !s.pin.open && !s.setupAsking && s.camtest == nil &&
		s.popup == nil && !s.updateActive && s.slideshow == nil && s.slideshowScreensaver == nil && !s.strip && s.cue == "" &&
		!s.streaming && !s.listening && s.sunrise == 0 && !s.announceRecording
}

// lunaIdleStart reuses only the static frame from the same second and facts.
// At 10 FPS the only painted regions are old/new sprite bounds and mini-eyes.
func (r *renderer) lunaIdleStart(s scene) (partial bool, finish func()) {
	allowed := lunaPlainIdle(s)
	r.zmu.Lock()
	asked := r.luna.walkAsked && allowed
	r.luna.signLive, r.luna.walkAsked = allowed, false
	r.zmu.Unlock()
	if asked {
		r.luna.animator.summon(s.now)
	}
	r.luna.animator.step(s.now, allowed)
	if !allowed {
		r.luna.key = ""
		return false, func() {}
	}
	key := fmt.Sprint(s.now.Truncate(time.Second), s.weather, s.style, s.timers, s.alarms, s.alerts, s.muted, s.callButton, s.missed, s.radio, s.glance, s.slideshowTrouble, skin(), r.font, r.w, r.h, walnut, amber, cream, dim, ember)
	if r.luna.base != nil && key == r.luna.key {
		eyes := image.Rect(r.s(54), r.s(50), r.s(122), r.s(82))
		draw.Draw(r.dst, eyes, r.luna.base, eyes.Min, draw.Src)
		if !r.luna.lastSprite.Empty() {
			b := r.luna.lastSprite
			draw.Draw(r.dst, b, r.luna.base, b.Min, draw.Src)
		}
		r.lunaIdleOverlay(s)
		return true, func() {}
	}
	return false, func() {
		if r.luna.base == nil || r.luna.base.Rect != r.dst.Rect {
			r.luna.base = image.NewRGBA(r.dst.Rect)
		}
		copy(r.luna.base.Pix, r.dst.Pix)
		r.luna.key = key
		r.lunaIdleOverlay(s)
	}
}

func (r *renderer) lunaIdleOverlay(s scene) {
	r.lunaMiniEyes(!r.luna.animator.blink.IsZero())
	r.luna.lastSprite = image.Rectangle{}
	x, frame, ok := r.luna.animator.sprite(s.now)
	if !ok {
		return
	}
	// Sprites have native dimensions: Show 5 never scales at runtime. The larger
	// panel's fixed-size sprite still walks the same logical ground line.
	sprite := lunaImage(fmt.Sprintf("walk_%02d", frame), 130, 95, false)
	at := image.Pt(r.s(x), r.s(339))
	b := sprite.Rect.Add(at)
	draw.Draw(r.dst, b, sprite, image.Point{}, draw.Over)
	r.luna.lastSprite = b.Intersect(r.dst.Rect)
}
