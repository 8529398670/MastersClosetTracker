// Package printer renders the 4x6 check-in ticket and sends it to the label
// printer. The fonts and logo are embedded and the whole PDF is built in
// memory; only the final PDF is briefly written to a temp file, because the OS
// print commands take a path.
//
// Ported from Master's Closet v2 (where the layout was tuned against the real
// label printer), so it keeps that code's formatting.
package printer

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"runtime"

	"log"

	"github.com/0187773933/MastersClosetTracker/server/settings"
	gofpdf "github.com/jung-kurt/gofpdf"
	barcode "github.com/ppsleep/barcode"
	code128 "github.com/ppsleep/barcode/code128"
)

//go:embed assets/ComicNeue-Regular.ttf
var fontBytes []byte

// Per-script fonts so non-Latin languages can render. ComicNeue covers Latin;
// these cover the scripts ComicNeue lacks. Selection is per language via
// TicketStrings.Font (see lang.go). Each is registered under a fixed name below.
//go:embed assets/NotoSans-Regular.ttf
var notoSansBytes []byte // Latin-ext + Cyrillic + Greek (ru/uk/el/vi)

//go:embed assets/NotoSansArabic-Regular.ttf
var notoArabicBytes []byte // Arabic script (ar/fa/ur)

//go:embed assets/NotoSansDevanagari-Regular.ttf
var notoDevanagariBytes []byte // Devanagari (hi)

//go:embed assets/logo.png
var logoBytes []byte

// Job is everything the ticket needs. Field names mirror v1's PrintJob so the
// layout port is faithful.
type Job struct {
	FamilySize         int    `json:"family_size"`
	TotalClothingItems int    `json:"total_clothing_items"`
	ClothingPerPerson  int    `json:"clothing_per_person"` // "( N ) Clothing Items" line; defaults to 6
	PantsLimit         int    `json:"pants_limit"`
	Shoes              int    `json:"shoes"`
	ShoesLimit         int    `json:"shoes_limit"`
	Accessories        int    `json:"accessories"`
	AccessoriesLimit   int    `json:"accessories_limit"`
	Seasonal           int    `json:"seasonal"`
	SeasonalLimit      int    `json:"seasonal_limit"`
	FamilyName         string `json:"family_name"`
	BarcodeNumber      string `json:"barcode_number"`
	Spanish            bool   `json:"spanish"`  // legacy; Language takes precedence
	Language           string `json:"language"` // ticket language code, e.g. "en", "es"
	Boys               int    `json:"boys"`
	Girls              int    `json:"girls"`
	Men                int    `json:"men"`
	Women              int    `json:"women"`
	Guests             int    `json:"guests"`
	// GuestNumber > 0 marks this as a guest's own ticket: it renders an extra
	// "Guest ( N )" line near the top but is otherwise a normal 1-person ticket,
	// carrying the host family's barcode so it scans to the same user.
	GuestNumber int `json:"guest_number"`
	// PassURL, when set, prints as a QR code beside the barcode: a phone that
	// scans it opens the family's offline pass (see static/site/home.js).
	PassURL string `json:"pass_url"`
}

// lang resolves the ticket language: explicit Language, then legacy Spanish bool.
func (j Job) lang() string {
	if j.Language != "" {
		return j.Language
	}
	if j.Spanish {
		return "es"
	}
	return "en"
}

// fitFont shrinks size (in 0.5pt steps, down to min) until text fits within maxW
// at the given font, so long non-English translations stay on one line instead
// of wrapping or running off the label. It leaves the chosen size active on the
// pdf and returns it.
func fitFont(pdf *gofpdf.Fpdf, text, fontName string, size, min, maxW float64) float64 {
	for size > min {
		pdf.SetFont(fontName, "", size)
		if pdf.GetStringWidth(text) <= maxW {
			return size
		}
		size -= 0.5
	}
	pdf.SetFont(fontName, "", min)
	return min
}

// centered draws text horizontally centered at y, auto-shrinking the font (down
// to min) so it fits maxW on a single line.
func centered(pdf *gofpdf.Fpdf, text, fontName string, size, min, maxW, y float64) {
	pageWidth, _ := pdf.GetPageSize()
	fitFont(pdf, text, fontName, size, min, maxW)
	x := (pageWidth / 2) - (pdf.GetStringWidth(text) / 2)
	pdf.Text(x, y, text)
}

// guestsLineFmt is the family ticket's guest-count format for n guests, picking
// singular vs plural and falling back to English for the languages that have no
// translated line yet.
func guestsLineFmt(t TicketStrings, n int) string {
	en := translations["en"]
	if n == 1 {
		if t.GuestsLineSingular != "" {
			return t.GuestsLineSingular
		}
		return en.GuestsLineSingular
	}
	if t.GuestsLinePlural != "" {
		return t.GuestsLinePlural
	}
	return en.GuestsLinePlural
}

func pluralText(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("( %d ) %s", n, singular)
	}
	return fmt.Sprintf("( %d ) %s", n, plural)
}

func BarcodePNG(number string) ([]byte, error) {
	if number == "" {
		number = "123456"
	}
	code, err := code128.A(number)
	if err != nil {
		return nil, err
	}
	img := barcode.Encode(code, 2, 50)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Render builds the ticket PDF in memory and returns its bytes. It is the single
// source of the ticket layout, used by both live printing and PDF previews.
func Render(cfg settings.Printer, job Job) ([]byte, error) {
	width := cfg.PageWidth
	if width <= 0 {
		width = 4
	}
	height := cfg.PageHeight
	if height <= 0 {
		height = 6
	}
	// The default (Latin) font; configurable, defaults to ComicNeue.
	latinFont := cfg.FontName
	if latinFont == "" {
		latinFont = "ComicNeue"
	}

	pdf := gofpdf.NewCustom(&gofpdf.InitType{
		UnitStr: "in",
		Size:    gofpdf.SizeType{Wd: width, Ht: height},
	})
	pdf.SetMargins(0.5, 1, 0.5)
	pdf.AddPage()
	// Register the Latin default plus the per-script fonts; the ticket language
	// picks which one to draw with (see TicketStrings.Font).
	pdf.AddUTF8FontFromBytes(latinFont, "", fontBytes)
	pdf.AddUTF8FontFromBytes("NotoSans", "", notoSansBytes)
	pdf.AddUTF8FontFromBytes("NotoSansArabic", "", notoArabicBytes)
	pdf.AddUTF8FontFromBytes("NotoSansDevanagari", "", notoDevanagariBytes)

	imgOpts := gofpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}

	// Logo.
	pdf.RegisterImageOptionsReader("logo", imgOpts, bytes.NewReader(logoBytes))
	pdf.ImageOptions("logo", 0.5, 0.10, 3, 0, false, imgOpts, 0, "")

	t := stringsFor(job.lang())
	// The font for this language's script; "" means the Latin default.
	fontName := t.Font
	if fontName == "" {
		fontName = latinFont
	}

	// Centered lines auto-fit within ~0.25in padding on each side; left-aligned
	// lines fit from their x to a small right padding. This keeps long
	// translations on one line at a slightly smaller size instead of wrapping.
	centerMaxW := width - 0.5
	rightEdge := width - 0.25

	// Two extra lines can appear, and each needs vertical room without pushing the
	// family name / barcode off their fixed positions near the bottom:
	//
	//   topShift   — a guest's own ticket gets a "Guest ( N )" line under the logo,
	//                which on its own would crowd it, so the whole upper block
	//                (guest line, family size, total, per-person) slides down.
	//   blockShift — a FAMILY ticket carrying guests gets a "Guests ( N )" count
	//                line under the total, so only the per-person block below it
	//                needs to move.
	//
	// A normal ticket with no guests is unaffected by both.
	topShift := 0.0
	if job.GuestNumber > 0 {
		topShift = 0.4
	}
	// Guests are counted on the family ticket only; a guest's own ticket is titled
	// with its number instead and must not also report the whole party.
	showGuestCount := job.GuestNumber == 0 && job.Guests > 0
	blockShift := topShift
	if showGuestCount {
		blockShift += 0.3
	}

	// Guest header: on a guest's own ticket, call it out above the family size.
	// Falls back to English when the language has no translated guest label.
	if job.GuestNumber > 0 {
		guestFmt := t.Guest
		if guestFmt == "" {
			guestFmt = "Guest ( %d )"
		}
		centered(pdf, fmt.Sprintf(guestFmt, job.GuestNumber), fontName, 18, 11, centerMaxW, 1.45+topShift)
	}

	// Family size + total.
	centered(pdf, fmt.Sprintf(t.FamilySize, job.FamilySize), fontName, 20, 12, centerMaxW, 1.8+topShift)
	centered(pdf, fmt.Sprintf(t.TotalItems, job.TotalClothingItems), fontName, 16, 9, centerMaxW, 2.1+topShift)

	// Guest count, between the total and the per-person block.
	if showGuestCount {
		centered(pdf, fmt.Sprintf(guestsLineFmt(t, job.Guests), job.Guests), fontName, 16, 9, centerMaxW, 2.4)
	}

	// Per-person limits.
	yStart, yStep := 2.5+blockShift, 0.3
	offset := 0.19
	indent := offset + 0.25
	indent2 := offset + 0.50
	clothingPP := job.ClothingPerPerson
	if clothingPP <= 0 {
		clothingPP = 6
	}
	clothingStr := fmt.Sprintf(t.ClothingItems, clothingPP)
	pantsStr := fmt.Sprintf(t.PantsLimit, job.PantsLimit)
	shoesStr := pluralText(job.ShoesLimit, t.ShoeSingular, t.ShoePlural)
	accStr := pluralText(job.AccessoriesLimit, t.AccessorySingular, t.AccessoryPlural)
	seasStr := pluralText(job.SeasonalLimit, t.SeasonalSingular, t.SeasonalPlural)

	// One shared size for the whole per-person block (kept uniform): shrink to
	// the largest size at which the worst-fitting line still fits.
	ppLines := []struct {
		x float64
		s string
	}{
		{offset, t.PerPerson}, {indent, clothingStr}, {indent2, pantsStr},
		{indent, shoesStr}, {indent, accStr}, {indent, seasStr},
	}
	ppSize := 12.0
	for _, ln := range ppLines {
		if s := fitFont(pdf, ln.s, fontName, 12, 8, rightEdge-ln.x); s < ppSize {
			ppSize = s
		}
	}
	pdf.SetFont(fontName, "", ppSize)
	pdf.Text(offset, yStart, t.PerPerson)
	pdf.Text(indent, yStart+yStep*1, clothingStr)
	pdf.Text(indent2, yStart+yStep*2, pantsStr)
	pdf.Text(indent, yStart+yStep*3, shoesStr)
	pdf.Text(indent, yStart+yStep*4, accStr)
	pdf.Text(indent, yStart+yStep*5, seasStr)

	bc, err := BarcodePNG(job.BarcodeNumber)
	if err != nil {
		return nil, fmt.Errorf("barcode: %w", err)
	}
	pdf.RegisterImageOptionsReader("barcode", imgOpts, bytes.NewReader(bc))

	if job.PassURL != "" {
		// Two columns under the per-person block (lowest baseline ~4.4in, on a
		// guest ticket): the phone pass QR on the left, big enough for a phone
		// camera, and the family name over the barcode on the right. The QR is
		// styled like the on-screen handoff QR, cross and all (see qr.go).
		qrPNG, err := handoffQRPNG(job.PassURL, handoffQRLevel)
		if err != nil {
			return nil, fmt.Errorf("qr code: %w", err)
		}
		const qrSize, qrLeft, qrTop = 1.2, 0.3, 4.6
		pdf.RegisterImageOptionsReader("pass", imgOpts, bytes.NewReader(qrPNG))
		pdf.ImageOptions("pass", qrLeft, qrTop, qrSize, qrSize, false, imgOpts, 0, "")

		colLeft := qrLeft + qrSize + 0.2
		colWidth := rightEdge - colLeft
		colCenter := colLeft + colWidth/2
		fitFont(pdf, job.FamilyName, fontName, 16, 8, colWidth)
		pdf.Text(colCenter-pdf.GetStringWidth(job.FamilyName)/2, 5.1, job.FamilyName)
		const barcodeWidth = 1.5
		pdf.ImageOptions("barcode", colCenter-barcodeWidth/2, 5.25, barcodeWidth, 0, false, imgOpts, 0, "")
	} else {
		centered(pdf, job.FamilyName, fontName, 16, 9, centerMaxW, 5.4)
		pdf.ImageOptions("barcode", 1.23, 5.5, 1.5, 0, false, imgOpts, 0, "")
	}

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}
	return out.Bytes(), nil
}

// Print renders the ticket and sends it to the configured printer. When no
// printer name is configured it renders only and returns printed=false (handy
// for dev/test boxes without the label printer attached). appDir is searched
// for SumatraPDF.exe on Windows.
func Print(cfg settings.Printer, appDir string, job Job) (printed bool, err error) {
	pdfBytes, err := Render(cfg, job)
	if err != nil {
		return false, err
	}
	if cfg.Name == "" {
		log.Println("[printer] no printer name set; ticket rendered but not sent")
		return false, nil
	}

	tmp, err := os.CreateTemp("", "mct-ticket-*.pdf")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(pdfBytes); err != nil {
		tmp.Close()
		return false, err
	}
	tmp.Close()
	return send(cfg, appDir, tmp.Name())
}

// send dispatches the PDF to the OS print system.
func send(cfg settings.Printer, appDir string, pdfPath string) (bool, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		// Best-effort queue clear so a stuck job doesn't block the next ticket.
		_ = exec.Command("cancel", "-a", cfg.Name).Run()
		speed := cfg.Speed
		if speed < 1 {
			speed = 2
		}
		cmd := exec.Command("lp", "-d", cfg.Name,
			"-o", fmt.Sprintf("PrintSpeed=%d", speed), "-o", "fit-to-page", pdfPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return false, fmt.Errorf("lp failed: %v: %s", err, bytes.TrimSpace(out))
		}
		return true, nil
	case "windows":
		// Always hand exec.Command an ABSOLUTE path: a bare name would go through
		// exec.LookPath, which since Go 1.19 refuses a hit in the working directory
		// (exec.ErrDot) — the exact regression from v1. See sumatra.go.
		exePath, tried := findSumatra(cfg.SumatraPath, appDir)
		if exePath == "" {
			return false, sumatraNotFound(tried)
		}
		cmd := exec.Command(exePath, "-print-to", cfg.Name, pdfPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return false, fmt.Errorf("%s failed: %v: %s", exePath, err, bytes.TrimSpace(out))
		}
		return true, nil
	default:
		return false, fmt.Errorf("printing unsupported on %s", runtime.GOOS)
	}
}
