package printer

// The ticket's phone sign-in QR code, drawn in the same style as the
// on-screen handoff QR (static/app/js/qr.js, which uses qr-code-styling):
// "classy-rounded" dots, "extra-rounded" corner squares, and the cross in the
// middle with the dots behind it cleared. The label printer is black and
// white, so it is black on white instead of maroon on grey.
//
// The shape rules are ported from qr-code-styling so the two read as the same
// code; the comments name the function each piece mirrors.

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"math"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed assets/cross.png
var crossBytes []byte // the same cross as static/app/img/verified.png

const (
	// qrModulePixels is how many pixels each module gets in the rendered PNG:
	// fine enough that the rounded shapes survive the printer's own scaling.
	qrModulePixels = 24
	// qrImageSize is the cross's share of the code, as in qr-code-styling's
	// imageOptions.imageSize default.
	qrImageSize = 0.4
	// qrImageMargin is the white gap around the cross, in modules (qr.js asks
	// for a 4px margin on a ~7px module).
	qrImageMargin = 0.5
)

// handoffQRLevel is the ticket QR's error correction: "Q", the same level
// qr-code-styling uses, so the cross can cover part of the code.
const handoffQRLevel = qrcode.High

// ecShare is how much of the code each error-correction level can lose,
// as qr-code-styling budgets it for the image.
var ecShare = map[qrcode.RecoveryLevel]float64{
	qrcode.Low: 0.07, qrcode.Medium: 0.15, qrcode.High: 0.25, qrcode.Highest: 0.30,
}

// hiddenDots is how many modules across and down the cross covers (mirrors
// qr-code-styling's calculateImageSize): odd counts so it sits dead center.
func hiddenDots(count int, aspect float64, level qrcode.RecoveryLevel) (across int, down int) {
	max_hidden := int(math.Floor(qrImageSize * ecShare[level] * float64(count*count)))
	max_axis := count - 14
	if max_hidden <= 0 {
		return 0, 0
	}
	across = int(math.Floor(math.Sqrt(float64(max_hidden) / aspect)))
	if across <= 0 {
		across = 1
	}
	if max_axis < across {
		across = max_axis
	}
	if across%2 == 0 {
		across--
	}
	down = 1 + 2*int(math.Ceil((float64(across)*aspect-1)/2))
	if down*across > max_hidden || max_axis < down {
		if max_axis < down {
			down = max_axis
			if down%2 == 0 {
				across--
			}
		} else {
			down -= 2
		}
		across = 1 + 2*int(math.Ceil((float64(down)/aspect-1)/2))
	}
	return across, down
}

// Shapes are tested in a module's own unit square (0..1, y down).

// inCornersRounded: a square with its bottom-left and top-right corners
// rounded (_basicCornersRounded).
func inCornersRounded(x, y float64) bool {
	if (x < 0.5 && y > 0.5) || (x > 0.5 && y < 0.5) {
		return math.Hypot(x-0.5, y-0.5) <= 0.5
	}
	return true
}

// inCornerExtraRounded: a quarter disc, round at the top right
// (_basicCornerExtraRounded).
func inCornerExtraRounded(x, y float64) bool {
	return math.Hypot(x, y-1) <= 1
}

// unrotate maps a point back through a quarter turn about the module's
// center: turns is +1 for a clockwise quarter turn on the page, -1 for
// counter-clockwise, as SVG's rotate() does with y pointing down.
func unrotate(x, y float64, turns int) (float64, float64) {
	dx, dy := x-0.5, y-0.5
	switch turns {
	case 1:
		dx, dy = dy, -dx
	case -1:
		dx, dy = -dy, dx
	}
	return 0.5 + dx, 0.5 + dy
}

// inClassyRounded is one "classy-rounded" dot, shaped by which neighbours it
// has (_drawClassyRounded).
func inClassyRounded(x, y float64, left, right, top, bottom bool) bool {
	switch {
	case !left && !right && !top && !bottom:
		ux, uy := unrotate(x, y, 1)
		return inCornersRounded(ux, uy)
	case !left && !top:
		ux, uy := unrotate(x, y, -1)
		return inCornerExtraRounded(ux, uy)
	case !right && !bottom:
		ux, uy := unrotate(x, y, 1)
		return inCornerExtraRounded(ux, uy)
	default:
		return true
	}
}

// inRoundedRect reports whether (x, y) is inside the square from lo to hi
// with corners of radius r.
func inRoundedRect(x, y, lo, hi, r float64) bool {
	if x < lo || x > hi || y < lo || y > hi {
		return false
	}
	cx := math.Max(lo+r, math.Min(x, hi-r))
	cy := math.Max(lo+r, math.Min(y, hi-r))
	return math.Hypot(x-cx, y-cy) <= r
}

// inExtraRoundedRing is the "extra-rounded" corner square, in the 7x7 finder's
// own coordinates (0..7): a rounded ring one module thick
// (_basicExtraRounded).
func inExtraRoundedRing(x, y float64) bool {
	return inRoundedRect(x, y, 0, 7, 2.5) && !inRoundedRect(x, y, 1, 6, 1.5)
}

// handoffQRPNG renders url as the styled, black-and-white handoff QR code.
func handoffQRPNG(url string, level qrcode.RecoveryLevel) ([]byte, error) {
	qr, err := qrcode.New(url, level)
	if err != nil {
		return nil, err
	}
	// The label itself is the quiet zone.
	qr.DisableBorder = true
	bits := qr.Bitmap()
	count := len(bits)

	cross, err := png.Decode(bytes.NewReader(crossBytes))
	if err != nil {
		return nil, err
	}
	cross_bounds := cross.Bounds()
	aspect := float64(cross_bounds.Dy()) / float64(cross_bounds.Dx())
	hide_across, hide_down := hiddenDots(count, aspect, level)

	inFinder := func(row, col int) bool {
		return (row < 7 && col < 7) || (row < 7 && col >= count-7) || (row >= count-7 && col < 7)
	}
	hidden := func(row, col int) bool {
		return 2*row >= count-hide_down && 2*row < count+hide_down &&
			2*col >= count-hide_across && 2*col < count+hide_across
	}
	// on is a module the dot pass draws: dark, not part of a corner, not
	// under the cross.
	on := func(row, col int) bool {
		if row < 0 || col < 0 || row >= count || col >= count {
			return false
		}
		return bits[row][col] && !inFinder(row, col) && !hidden(row, col)
	}

	size := count * qrModulePixels
	canvas := image.NewGray(image.Rect(0, 0, size, size))
	for i := range canvas.Pix {
		canvas.Pix[i] = 0xff
	}
	black := color.Gray{Y: 0}

	// Dots.
	for row := 0; row < count; row++ {
		for col := 0; col < count; col++ {
			if !on(row, col) {
				continue
			}
			left, right := on(row, col-1), on(row, col+1)
			top, bottom := on(row-1, col), on(row+1, col)
			for py := 0; py < qrModulePixels; py++ {
				for px := 0; px < qrModulePixels; px++ {
					x := (float64(px) + 0.5) / qrModulePixels
					y := (float64(py) + 0.5) / qrModulePixels
					if inClassyRounded(x, y, left, right, top, bottom) {
						canvas.SetGray(col*qrModulePixels+px, row*qrModulePixels+py, black)
					}
				}
			}
		}
	}

	// Corners: the extra-rounded ring, and the 3x3 center as classy-rounded
	// dots (their outer corners round, the rest square).
	for _, origin := range [][2]int{{0, 0}, {0, count - 7}, {count - 7, 0}} {
		top_px, left_px := origin[0]*qrModulePixels, origin[1]*qrModulePixels
		for py := 0; py < 7*qrModulePixels; py++ {
			for px := 0; px < 7*qrModulePixels; px++ {
				x := (float64(px) + 0.5) / qrModulePixels
				y := (float64(py) + 0.5) / qrModulePixels
				dark := inExtraRoundedRing(x, y)
				if !dark && x >= 2 && x < 5 && y >= 2 && y < 5 {
					col, row := int(x)-2, int(y)-2
					dark = inClassyRounded(x-math.Floor(x), y-math.Floor(y), col > 0, col < 2, row > 0, row < 2)
				}
				if dark {
					canvas.SetGray(left_px+px, top_px+py, black)
				}
			}
		}
	}

	// The cross, centered over the cleared modules, inside its margin.
	box_w := float64(hide_across) - 2*qrImageMargin
	box_h := float64(hide_down) - 2*qrImageMargin
	if box_w > 0 && box_h > 0 {
		// Fit the cross's own aspect inside the box.
		draw_w, draw_h := box_w, box_w*aspect
		if draw_h > box_h {
			draw_w, draw_h = box_h/aspect, box_h
		}
		w_px := int(draw_w * qrModulePixels)
		h_px := int(draw_h * qrModulePixels)
		left_px := (size - w_px) / 2
		top_px := (size - h_px) / 2
		for py := 0; py < h_px; py++ {
			for px := 0; px < w_px; px++ {
				sx := cross_bounds.Min.X + px*cross_bounds.Dx()/w_px
				sy := cross_bounds.Min.Y + py*cross_bounds.Dy()/h_px
				red, green, blue, alpha := cross.At(sx, sy).RGBA()
				if alpha > 0x8000 && (red+green+blue)/3 < alpha/2 {
					canvas.SetGray(left_px+px, top_px+py, black)
				}
			}
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
