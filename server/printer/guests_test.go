package printer

import (
	"bytes"
	"testing"

	"github.com/0187773933/MastersClosetTracker/server/settings"
)

// TestGuestsLinePluralizes checks every language reads singular for one guest
// and plural otherwise, with its own translation rather than English.
func TestGuestsLinePluralizes(t *testing.T) {
	en := translations["en"]
	for _, l := range langOrder {
		ts := stringsFor(l.Code)
		if ts.GuestsLineSingular == "" || ts.GuestsLinePlural == "" || ts.Guest == "" {
			t.Errorf("%s: missing guest strings", l.Code)
		}
		if l.Code != "en" && ts.GuestsLinePlural == en.GuestsLinePlural {
			t.Errorf("%s: guest line is untranslated", l.Code)
		}
		if guestsLineFmt(ts, 1) != ts.GuestsLineSingular || guestsLineFmt(ts, 2) != ts.GuestsLinePlural {
			t.Errorf("%s: wrong form chosen", l.Code)
		}
	}
}

// TestRenderWithPassQR renders a family and a guest ticket carrying the
// phone pass QR code beside the barcode.
func TestRenderWithPassQR(t *testing.T) {
	job := Job{
		FamilySize: 3, TotalClothingItems: 18, ClothingPerPerson: 6, FamilyName: "Smith",
		BarcodeNumber: "9123456789", Guests: 2,
		PassURL: "https://masterscloset.cc/p#9123456789",
	}
	for _, guest := range []int{0, 1} {
		job.GuestNumber = guest
		pdf, err := Render(settings.Printer{}, job)
		if err != nil {
			t.Fatalf("guest %d: %v", guest, err)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF")) {
			t.Fatalf("guest %d: not a PDF", guest)
		}
	}
}

// TestHandoffQRCrossArea checks the cleared area under the cross is centered
// (odd on both axes) and stays within what the error correction can lose.
func TestHandoffQRCrossArea(t *testing.T) {
	for _, count := range []int{41, 45, 49, 53} {
		across, down := hiddenDots(count, 98.0/107.0, handoffQRLevel)
		if across%2 == 0 || down%2 == 0 || across <= 0 || down <= 0 {
			t.Fatalf("%d modules: cleared %dx%d is not centered", count, across, down)
		}
		if float64(across*down) > qrImageSize*ecShare[handoffQRLevel]*float64(count*count) {
			t.Fatalf("%d modules: cleared %dx%d is more than the code can lose", count, across, down)
		}
	}
	if _, err := handoffQRPNG("https://closet.example.org/g/c.x.y.z", handoffQRLevel); err != nil {
		t.Fatal(err)
	}
}
