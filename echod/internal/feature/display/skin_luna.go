//go:build !dot && !spot

package display

import (
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/vector"
)

//go:embed assets/luna/*.png
var lunaAssets embed.FS

var (
	lunaCyan    = rgb(0x22E4FF)
	lunaText    = rgb(0xE6F7FF)
	lunaDim     = rgb(0x8BA6BF)
	lunaRules   = rgb(0x1C2C44)
	lunaMagenta = rgb(0xFF3FD8)
	lunaAmber   = rgb(0xFFB547)
)

// The PNGs stay untouched. Only these cached background copies have scanlines;
// text, sprites and system indicators never pass through a panel filter.
var lunaImages struct {
	sync.Mutex
	by map[string]*image.RGBA
}

func lunaImage(name string, w, h int, scan bool) *image.RGBA {
	lunaImages.Lock()
	defer lunaImages.Unlock()
	key := fmt.Sprintf("%s/%d/%d/%t", name, w, h, scan)
	if img := lunaImages.by[key]; img != nil {
		return img
	}
	f, err := lunaAssets.Open("assets/luna/" + name + ".png")
	if err != nil {
		panic(err)
	} // embedded, checked by the asset test
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if src.Bounds().Size() == img.Bounds().Size() {
		draw.Draw(img, img.Rect, src, src.Bounds().Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(img, img.Rect, src, src.Bounds(), draw.Src, nil)
	}
	if scan {
		for y := 1; y < h; y += 2 {
			for x := 0; x < w; x++ {
				i := img.PixOffset(x, y)
				for c := 0; c < 3; c++ {
					img.Pix[i+c] = uint8(uint16(img.Pix[i+c]) * 92 / 100)
				}
			}
		}
	}
	if lunaImages.by == nil {
		lunaImages.by = make(map[string]*image.RGBA)
	}
	lunaImages.by[key] = img
	return img
}

// lunaSkyline is shared with the turn renderer. dimmed selects the conversation
// and ringing asset; it does not change any system/night palette.
func (r *renderer) lunaSkyline(dimmed bool) {
	name := "skyline-idle"
	if dimmed {
		name = "skyline-dim"
	}
	draw.Draw(r.dst, r.dst.Rect, lunaImage(name, r.w, r.h, true), image.Point{}, draw.Src)
}

func (r *renderer) lunaBackdrop(s scene) {
	if nightNow(s.now) {
		draw.Draw(r.dst, r.dst.Rect, image.NewUniform(rgb(0x070B14)), image.Point{}, draw.Src)
		return
	}
	r.lunaSkyline(false)
	washOver(r.dst, image.Rect(r.s(56), r.s(430), r.s(896), r.s(430)+max(1, r.s(1))), faded(lunaCyan, 102))
	for x := 56; x <= 896; x += 64 {
		r.stroke(r.s(x), r.s(424), r.s(x), r.s(436), max(1, r.s(1)), lunaRules)
	}
}

// Fixed design sizes deliberately ignore the user's generic clock font; Luna's
// Saira and Go Mono faces are cached independently (no font allocation per tick).
type lunaFaceKey struct {
	name string
	bold bool
	size int
}

func (r *renderer) lunaFace(bold bool, size int) font.Face { return r.lunaFont("saira", bold, size) }
func (r *renderer) lunaMono(size int) font.Face            { return r.lunaFont("gomono", false, size) }
func (r *renderer) lunaFont(name string, bold bool, size int) font.Face {
	k := lunaFaceKey{name, bold, max(1, r.s(size))}
	if r.luna.faces == nil {
		r.luna.faces = make(map[lunaFaceKey]font.Face)
	}
	if f := r.luna.faces[k]; f != nil {
		return f
	}
	// fontFace's family normalization matches generic Go layouts. Luna's
	// design uses actual font pixels, so undo that scale for these fixed faces.
	scale := 0.98
	if name == "gomono" {
		scale = 0.755
	}
	f := fontFace(name, bold, float64(k.size)/scale)
	r.luna.faces[k] = f
	return f
}

func (r *renderer) lunaMid(face font.Face, text string, x, y int, c color.Color) {
	b := inkOf(face, text)
	r.text(face, text, r.s(x), r.s(y)-(b.Min.Y+b.Max.Y)/2, c)
}

// lunaGlowArc is a band with two cheap halo bands, not a blur.
func (r *renderer) lunaGlowArc(cx, cy, rad, width, from, to float64, c color.RGBA) {
	r.arcBand(cx, cy, rad, width+float64(r.s(10)), from, to, faded(c, 26))
	r.arcBand(cx, cy, rad, width+float64(r.s(4)), from, to, faded(c, 64))
	r.arcBand(cx, cy, rad, width, from, to, c)
}

// lunaOval fills an ellipse; all arguments are logical design pixels.
func (r *renderer) lunaOval(cx, cy, rx, ry int, c color.RGBA) {
	b := image.Rect(r.s(cx-rx), r.s(cy-ry), r.s(cx+rx), r.s(cy+ry))
	r.fillShape(b, c, func(z *vector.Rasterizer, w, h float32) {
		const k = 0.55228475
		x, y := w/2, h/2
		z.MoveTo(w, y)
		z.CubeTo(w, y+k*y, x+k*x, h, x, h)
		z.CubeTo(x-k*x, h, 0, y+k*y, 0, y)
		z.CubeTo(0, y-k*y, x-k*x, 0, x, 0)
		z.CubeTo(x+k*x, 0, w, y-k*y, w, y)
		z.ClosePath()
	})
}

// Upper-lid arcs (not smiles): 200° to 340° of a 24 × 18 ellipse, as the reference draws them. A
// blink closes each lid to a line at the arc's ends.
func (r *renderer) lunaMiniEyes(closed bool) {
	for _, cx := range []float64{70, 106} {
		var lid [][2]float64
		for i := 0; i <= 14; i++ {
			a := (200 + 10*float64(i)) * math.Pi / 180
			y := 68 + 9*math.Sin(a)
			if closed {
				y = 65
			}
			lid = append(lid, [2]float64{cx + 12*math.Cos(a), y})
		}
		r.lunaTrace(lid, 3, faded(lunaCyan, 153))
	}
}

// lunaTrace strokes a polyline in logical pixels as one antialiased shape, so a see-through color
// covers each pixel once, where stroke's overlapping dabs would add up to nearly solid.
func (r *renderer) lunaTrace(pts [][2]float64, width float64, c color.RGBA) {
	if len(pts) < 2 {
		return
	}
	k, half := r.sk(), width*r.sk()/2
	box := image.Rectangle{}
	for _, p := range pts {
		box = box.Union(image.Rect(int(p[0]*k-half)-1, int(p[1]*k-half)-1, int(p[0]*k+half)+2, int(p[1]*k+half)+2))
	}
	box = box.Intersect(r.dst.Rect)
	// Each point moves half the width either way along the line's normal there.
	edge := func(i int, side float64) (float32, float32) {
		p, q := pts[max(0, i-1)], pts[min(len(pts)-1, i+1)]
		dx, dy := q[0]-p[0], q[1]-p[1]
		l := math.Hypot(dx, dy)
		if l == 0 {
			l = 1
		}
		return float32(pts[i][0]*k-side*half*dy/l) - float32(box.Min.X), float32(pts[i][1]*k+side*half*dx/l) - float32(box.Min.Y)
	}
	r.fillShape(box, c, func(z *vector.Rasterizer, _, _ float32) {
		z.MoveTo(edge(0, 1))
		for i := 1; i < len(pts); i++ {
			z.LineTo(edge(i, 1))
		}
		for i := len(pts) - 1; i >= 0; i-- {
			z.LineTo(edge(i, -1))
		}
	})
}
