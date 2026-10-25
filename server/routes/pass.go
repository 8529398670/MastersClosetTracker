package routes

import (
	fiber "github.com/gofiber/fiber/v3"

	index "github.com/0187773933/MastersClosetTracker/server/index"
	security "github.com/0187773933/MastersClosetTracker/server/security"
)

// A family's phone pass lives entirely on the public site (mct-site,
// static/site/): the ticket's QR code links to <public site>/p#<barcode>,
// and the part after "#" never reaches any server. The page draws the
// family's barcode for the front desk scanner and keeps it on the phone.
// So nothing here -- or anywhere on the internet -- answers questions about
// families; this file only builds those links.

func ( handlers *Handlers ) actor( c fiber.Ctx ) ( actor *security.Actor ) {
	actor = security.ActorFrom( c )
	return
}

// familyBarcode is the number the family's tickets carry: their first card.
func familyBarcode( client index.Client ) ( barcode string ) {
	if len( client.Record.Barcodes ) > 0 {
		barcode = client.Record.Barcodes[ 0 ]
	}
	return
}

// passURL is the link a family's phone opens to keep their pass, or "" when
// no public site is configured or there is no barcode to carry.
func ( handlers *Handlers ) passURL( barcode string ) ( url string ) {
	if handlers.Config.SitePublicURL == "" || barcode == "" { return }
	url = handlers.Config.SitePublicURL + "/p#" + barcode
	return
}

// SiteInfo tells the staff pages where the public site is, so they can draw
// a family's pass link without asking the server for each one.
func ( handlers *Handlers ) SiteInfo( c fiber.Ctx ) ( err error ) {
	err = c.JSON( fiber.Map{ "public_url": handlers.Config.SitePublicURL } )
	return
}
