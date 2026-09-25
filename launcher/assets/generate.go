//go:build ignore

// MKYBOOT launcher icon generator.
//
// Usage:
//
//	go run generate.go [output.ico]
//
// Renders a project-owned geometric icon: a dark rounded square with a
// blue power/boot ring, light center bar and a small green status dot -
// a clean boot/server concept that stays recognizable at 16px tray sizes.
// Frames are PNG-compressed into a standard .ico container.

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var (
	colBG     = color.RGBA{18, 21, 28, 255}
	colRing   = color.RGBA{59, 130, 246, 255}
	colBar    = color.RGBA{234, 241, 255, 255}
	colStatus = color.RGBA{34, 197, 94, 255}
)

// insideRoundedSquare reports whether the point lies inside the rounded
// square (inset 4.5%, corner radius 24% of the canvas).
func insideRoundedSquare(x, y, s float64) bool {
	inset := 0.045 * s
	radius := 0.24 * s
	l, t, r, b := inset, inset, s-inset, s-inset
	if x < l || x > r || y < t || y > b {
		return false
	}
	corners := [][2]float64{{l + radius, t + radius}, {r - radius, t + radius},
		{l + radius, b - radius}, {r - radius, b - radius}}
	inCornerX := x < l+radius || x > r-radius
	inCornerY := y < t+radius || y > b-radius
	for _, c := range corners {
		nearX := (c[0] == l+radius && x < c[0]) || (c[0] == r-radius && x > c[0])
		nearY := (c[1] == t+radius && y < c[1]) || (c[1] == b-radius && y > c[1])
		if nearX && nearY && math.Hypot(x-c[0], y-c[1]) > radius {
			return false
		}
	}
	_ = inCornerX
	_ = inCornerY
	return true
}

// render draws one icon frame at size*ss then box-downsamples for antialiasing.
func render(size, ss int) *image.RGBA {
	n := size * ss
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	s := float64(n)
	cx, cy := s/2, s/2
	ringR := 0.285 * s
	ringT := 0.105 * s
	gapHalf := 30.0 * math.Pi / 180.0 // gap at the top of the ring
	barW := 0.105 * s
	barTop := cy - ringR - ringT*0.3
	barBot := cy + ringR*0.12
	dotX, dotY, dotR := s*0.755, s*0.255, 0.062*s

	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if !insideRoundedSquare(fx, fy, s) {
				continue // transparent
			}
			c := colBG
			dx, dy := fx-cx, fy-cy
			d := math.Hypot(dx, dy)
			if d >= ringR-ringT/2 && d <= ringR+ringT/2 {
				ang := math.Atan2(dx, -dy) // 0 = up, grows clockwise
				if math.Abs(ang) > gapHalf {
					c = colRing
				}
			}
			if math.Abs(fx-cx) <= barW/2 && fy >= barTop && fy <= barBot {
				c = colBar
			}
			if math.Hypot(fx-dotX, fy-dotY) <= dotR {
				c = colStatus
			}
			img.SetRGBA(x, y, c)
		}
	}
	return downsample(img, size)
}

// downsample box-filters a supersampled image to the target size.
func downsample(src *image.RGBA, size int) *image.RGBA {
	scale := src.Rect.Dx() / size
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	if scale < 1 {
		scale = 1
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var rs, gs, bs, as uint32
			for sy := 0; sy < scale; sy++ {
				for sx := 0; sx < scale; sx++ {
					p := src.RGBAAt(x*scale+sx, y*scale+sy)
					rs += uint32(p.R)
					gs += uint32(p.G)
					bs += uint32(p.B)
					as += uint32(p.A)
				}
			}
			n := uint32(scale * scale)
			dst.SetRGBA(x, y, color.RGBA{
				uint8(rs / n), uint8(gs / n), uint8(bs / n), uint8(as / n),
			})
		}
	}
	return dst
}

// frame is one PNG-encoded ICO entry.
type frame struct {
	size int
	data []byte
}

func mainFrame(size int) frame {
	ss := 8
	if size >= 128 {
		ss = 4
	}
	img := render(size, ss)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return frame{size: size, data: buf.Bytes()}
}

func writeICO(path string, frames []frame) error {
	var out bytes.Buffer
	// ICONDIR
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // icon
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(frames)))
	offset := 6 + 16*len(frames)
	for _, f := range frames {
		dim := byte(f.size)
		if f.size >= 256 {
			dim = 0
		}
		out.WriteByte(dim)
		out.WriteByte(dim)
		out.WriteByte(0)                                        // colors
		out.WriteByte(0)                                        // reserved
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // planes
		_ = binary.Write(&out, binary.LittleEndian, uint16(32)) // bpp
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(f.data)))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(f.data)
	}
	for _, f := range frames {
		out.Write(f.data)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func main() {
	out := "mkyboot.ico"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	sizes := []int{16, 24, 32, 48, 64, 256}
	frames := make([]frame, 0, len(sizes))
	for _, s := range sizes {
		frames = append(frames, mainFrame(s))
	}
	if err := writeICO(out, frames); err != nil {
		panic(err)
	}
	// Also emit PNG frames for go-winres (RT_GROUP_ICON accepts PNG lists).
	dir := filepath.Dir(out)
	for _, f := range frames {
		switch f.size {
		case 16, 32, 48, 256:
			name := filepath.Join(dir, fmt.Sprintf("mkyboot_%d.png", f.size))
			if err := os.WriteFile(name, f.data, 0o644); err != nil {
				panic(err)
			}
		}
	}
	// Render a preview PNG into the system temp directory so the design
	// can be inspected without an ICO decoder (never written into the repo).
	preview := render(128, 8)
	tmp := os.TempDir() + string(os.PathSeparator) + "mkyboot_icon_preview.png"
	f, err := os.Create(tmp)
	if err == nil {
		_ = png.Encode(f, preview)
		_ = f.Close()
	}
}
