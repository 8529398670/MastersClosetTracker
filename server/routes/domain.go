package routes

import (
	errors "errors"
	strings "strings"

	fiber "github.com/gofiber/fiber/v3"

	closet "github.com/0187773933/MastersClosetTracker/server/closet"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	settings "github.com/0187773933/MastersClosetTracker/server/settings"
)

// Domain is what the closet's own routes need on top of the template's.
type Domain struct {
	Closet   *closet.Service
	Settings *settings.Service
	Docs     *docs.Store
}

// registerDomain wires the closet's pages and API. Pages are registered by
// registerPublic, before the template's "/api" group; the staff API is
// registered after it and inherits RequireLogin from "/api" and RequireAdmin
// from "/api/admin". The station has no family-facing API at all: families
// only ever reach the public site (cmd/site), which is static.
func ( handlers *Handlers ) registerPublic( app *fiber.App ) {
	if handlers.Domain == nil { return }
	handlers.registerPages( app )
}

func ( handlers *Handlers ) registerStaffAPI( app *fiber.App ) {
	if handlers.Domain == nil { return }
	csrf := handlers.Guard.RequireCSRFHeader

	clients := app.Group( "/api/clients" , csrf )
	clients.Get( "/recent" , handlers.RecentClients )
	clients.Get( "/search" , handlers.SearchClients )
	clients.Get( "/by-barcode" , handlers.ClientByBarcode )
	clients.Post( "" , handlers.CreateClient )
	clients.Get( "/:client_id" , handlers.GetClient )
	clients.Post( "/:client_id" , handlers.UpdateClient )
	clients.Get( "/:client_id/similar" , handlers.SimilarClients )
	clients.Get( "/:client_id/checkin" , handlers.CheckInScreen )
	clients.Post( "/:client_id/visits" , handlers.RecordVisit )

	visit_routes := app.Group( "/api/visits" , csrf )
	visit_routes.Get( "/days" , handlers.VisitDays )
	visit_routes.Get( "/export.xlsx" , handlers.ExportAll )
	visit_routes.Get( "/days/:day" , handlers.VisitDay )
	visit_routes.Get( "/days/:day/export.xlsx" , handlers.ExportDay )
	visit_routes.Post( "/days/:day/manual" , handlers.SetDayTally )
	visit_routes.Get( "/:visit_id/ticket.pdf" , handlers.TicketPDF )
	visit_routes.Post( "/:visit_id/print" , handlers.ReprintVisit )

	app.Get( "/api/print/languages" , handlers.PrintLanguages )
	app.Get( "/api/print/strings" , handlers.PrintStrings )
	app.Get( "/api/limits" , handlers.Limits )
	app.Get( "/api/settings" , handlers.GetSettings )
	app.Get( "/api/site" , handlers.SiteInfo )
}

func ( handlers *Handlers ) registerAdminAPI( app *fiber.App ) {
	if handlers.Domain == nil { return }
	csrf := handlers.Guard.RequireCSRFHeader
	// Deletes need a browser session, like deleting an account: nothing on
	// the pages can undo one, so a leaked admin API key must not be able to
	// erase families or their history.
	session := handlers.Guard.RequireSession
	app.Post( "/api/admin/clients/:client_id/delete" , csrf , session , handlers.DeleteClient )
	app.Post( "/api/admin/visits/:visit_id/delete" , csrf , session , handlers.DeleteVisit )
	app.Post( "/api/admin/settings" , csrf , handlers.UpdateSettings )
}

// closetError turns a closet error into the one response shape the pages
// handle: a status and {"error": ...}.
func closetError( c fiber.Ctx , err error ) ( response error ) {
	switch {
	case errors.Is( err , closet.ErrNotFound ):
		response = notFound( c , "not found" )
	case errors.Is( err , closet.ErrStale ) , errors.Is( err , closet.ErrRecentVisit ):
		response = c.Status( fiber.StatusConflict ).JSON( fiber.Map{ "error": err.Error() , "result": false } )
	case errors.Is( err , closet.ErrInvalid ):
		response = badRequest( c , err.Error() )
	default:
		response = serverError( c )
	}
	return
}

// param reads a path parameter as an owned string -- it may be kept (as a
// record id, an index key), so it must not alias the request buffer even if
// the app is ever run without fiber's Immutable setting.
func param( c fiber.Ctx , name string ) ( value string ) {
	value = strings.Clone( strings.TrimSpace( c.Params( name ) ) )
	return
}
