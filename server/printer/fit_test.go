package printer

import (
	"fmt"
	"testing"

	gofpdf "github.com/jung-kurt/gofpdf"
)

// newFittingPDF builds a pdf with the same page + fonts Render uses, so width
// measurements in the test match what the real ticket produces.
func newFittingPDF() *gofpdf.Fpdf {
	pdf := gofpdf.NewCustom(&gofpdf.InitType{
		UnitStr: "in",
		Size:    gofpdf.SizeType{Wd: 4, Ht: 6},
	})
	pdf.AddPage()
	pdf.AddUTF8FontFromBytes("ComicNeue", "", fontBytes)
	pdf.AddUTF8FontFromBytes("NotoSans", "", notoSansBytes)
	pdf.AddUTF8FontFromBytes("NotoSansArabic", "", notoArabicBytes)
	pdf.AddUTF8FontFromBytes("NotoSansDevanagari", "", notoDevanagariBytes)
	return pdf
}

// TestTicketLinesFit checks that, after auto-fitting, every line of every
// language fits the label width on a single line (no wrap / overflow).
func TestTicketLinesFit(t *testing.T) {
	const width = 4.0
	centerMaxW := width - 0.5
	rightEdge := width - 0.25
	pdf := newFittingPDF()

	for _, l := range langOrder {
		ts := stringsFor(l.Code)
		fontName := ts.Font
		if fontName == "" {
			fontName = "ComicNeue"
		}

		check := func(label, text string, size, min, maxW float64) {
			got := fitFont(pdf, text, fontName, size, min, maxW)
			pdf.SetFont(fontName, "", got)
			if w := pdf.GetStringWidth(text); w > maxW+0.001 {
				t.Errorf("%s %s: %q does not fit (width %.2f > %.2f at size %.1f, min %.1f)",
					l.Code, label, text, w, maxW, got, min)
			}
		}

		check("family", fmt.Sprintf(ts.FamilySize, 8), 20, 12, centerMaxW)
		check("guest1", fmt.Sprintf(guestsLineFmt(ts, 1), 1), 16, 9, centerMaxW)
		check("guests", fmt.Sprintf(guestsLineFmt(ts, 8), 8), 16, 9, centerMaxW)
		check("total", fmt.Sprintf(ts.TotalItems, 88), 16, 9, centerMaxW)
		check("perperson", ts.PerPerson, 12, 8, rightEdge-0.19)
		check("clothing", fmt.Sprintf(ts.ClothingItems, 6), 12, 8, rightEdge-0.44)
		check("pants", fmt.Sprintf(ts.PantsLimit, 2), 12, 8, rightEdge-0.69)
		check("shoes", pluralText(2, ts.ShoeSingular, ts.ShoePlural), 12, 8, rightEdge-0.44)
		check("accessory", pluralText(2, ts.AccessorySingular, ts.AccessoryPlural), 12, 8, rightEdge-0.44)
		check("seasonal", pluralText(2, ts.SeasonalSingular, ts.SeasonalPlural), 12, 8, rightEdge-0.44)
	}
}
