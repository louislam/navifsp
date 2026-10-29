//go:build ignore

// genicon writes ../icon.ico: a blue rounded square with a white music note.
// Run from the server directory: go run tools/genicon/main.go [preview.png]
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// sizes holds every image stored in the .ico. 16/32/48 are DIBs for the shell
// and the notification area, 256 is a PNG for large icon views.
var sizes = []int{16, 32, 48, 256}

var (
	bg = color.RGBA{0x3B, 0x5B, 0xF6, 0xFF}
	fg = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// inside reports the colour at a point of the 32x32 design grid: the rounded
// background square or the note drawn on top of it.
func inside(x, y float64) (color.RGBA, bool) {
	// The note sits slightly right of centre to leave room for its head.
	nx, ny := x-2.0, y-0.6
	// Note head: a slanted ellipse.
	hx, hy := nx-12.0, ny-22.6
	cos, sin := math.Cos(0.4), math.Sin(0.4)
	ex := hx*cos - hy*sin
	ey := hx*sin + hy*cos
	if (ex*ex)/(4.2*4.2)+(ey*ey)/(2.8*2.8) <= 1 {
		return fg, true
	}
	// Stem.
	if inRoundedRect(nx, ny, 13.0, 8.0, 15.6, 22.6, 1.0) {
		return fg, true
	}
	// Flag: a thick stroke along a curve from the stem to the right.
	if distToCurve(nx, ny, 14.3, 8.4, 21.0, 10.6, 21.0, 16.4) <= 1.35 {
		return fg, true
	}
	if inRoundedRect(x, y, 1, 1, 31, 31, 7) {
		return bg, true
	}
	return color.RGBA{}, false
}

func distToCurve(px, py, x0, y0, cx, cy, x1, y1 float64) float64 {
	best := math.Inf(1)
	for i := 0; i <= 64; i++ {
		t := float64(i) / 64
		u := 1 - t
		x := u*u*x0 + 2*u*t*cx + t*t*x1
		y := u*u*y0 + 2*u*t*cy + t*t*y1
		if d := math.Hypot(px-x, py-y); d < best {
			best = d
		}
	}
	return best
}

func inRoundedRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx, cy := x, y
	switch {
	case x < x0+r && y < y0+r:
		cx, cy = x0+r, y0+r
	case x > x1-r && y < y0+r:
		cx, cy = x1-r, y0+r
	case x < x0+r && y > y1-r:
		cx, cy = x0+r, y1-r
	case x > x1-r && y > y1-r:
		cx, cy = x1-r, y1-r
	default:
		return true
	}
	return math.Hypot(x-cx, y-cy) <= r
}

// render draws the icon at px pixels wide with 4x4 supersampling, so small
// sizes still look smooth.
func render(px int) *image.RGBA {
	const ss = 4
	scale := 32.0 / float64(px)
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					gx := (float64(x) + (float64(sx)+0.5)/ss) * scale
					gy := (float64(y) + (float64(sy)+0.5)/ss) * scale
					c, ok := inside(gx, gy)
					if !ok {
						continue
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					a++
				}
			}
			if a == 0 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(r / a),
				G: uint8(g / a),
				B: uint8(b / a),
				A: uint8(a / (ss * ss) * 255),
			})
		}
	}
	return img
}

// dibImage encodes one image as an uncompressed icon DIB: every Windows version
// accepts it, unlike PNG entries in older dialogs.
func dibImage(img *image.RGBA) []byte {
	px := img.Bounds().Dx()
	var b bytes.Buffer
	// BITMAPINFOHEADER, height doubled because of the AND mask
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(px))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(px*2))
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	binary.LittleEndian.PutUint32(hdr[20:], uint32(px*px*4+maskRowBytes(px)*px))
	b.Write(hdr)
	for y := px - 1; y >= 0; y-- { // bottom-up, BGRA
		for x := 0; x < px; x++ {
			c := img.RGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	b.Write(make([]byte, maskRowBytes(px)*px)) // AND mask: opaque
	return b.Bytes()
}

func maskRowBytes(px int) int {
	return ((px + 31) / 32) * 4
}

// writeICO stores each size: 256 pixels as PNG (the shell expects it), smaller
// sizes as DIBs.
func writeICO(path string, images map[int]*image.RGBA) {
	type entry struct {
		px   int
		data []byte
	}
	var entries []entry
	for _, px := range sizes {
		img := images[px]
		if px >= 128 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				panic(err)
			}
			entries = append(entries, entry{px, buf.Bytes()})
			continue
		}
		entries = append(entries, entry{px, dibImage(img)})
	}

	out := &bytes.Buffer{}
	binary.Write(out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(out, binary.LittleEndian, uint16(1)) // icon
	binary.Write(out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		dim := byte(e.px)
		if e.px >= 256 {
			dim = 0 // 0 means 256
		}
		out.Write([]byte{dim, dim, 0, 0})
		binary.Write(out, binary.LittleEndian, uint16(1))  // planes
		binary.Write(out, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(out, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(out, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}

	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		panic(err)
	}
}

func main() {
	images := make(map[int]*image.RGBA, len(sizes))
	for _, px := range sizes {
		images[px] = render(px)
	}
	// icon.ico lives in the server directory, so run this from there.
	writeICO("icon.ico", images)

	if len(os.Args) > 1 {
		f, err := os.Create(os.Args[1])
		if err != nil {
			panic(err)
		}
		defer f.Close()
		big := image.NewRGBA(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				big.SetRGBA(x, y, images[256].RGBAAt(x, y))
			}
		}
		png.Encode(f, big)
	}
}
