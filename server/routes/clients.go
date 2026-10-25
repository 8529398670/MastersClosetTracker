package routes

import (
	json "encoding/json"
	strconv "strconv"
	strings "strings"

	fiber "github.com/gofiber/fiber/v3"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	index "github.com/0187773933/MastersClosetTracker/server/index"
	views "github.com/0187773933/MastersClosetTracker/server/views"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

// Families: list, search, look up, create, edit, delete. Every response is
// shaped by server/views for the existing pages.

func ( handlers *Handlers ) userView( client index.Client ) ( view map[string]any ) {
	standing , err := handlers.Domain.Closet.Standing( client.ID )
	location := handlers.Domain.Closet.Location()
	if err != nil {
		view = views.User( client , nil , location )
		return
	}
	view = views.User( standing.Client , &standing , location )
	return
}

func ( handlers *Handlers ) summary( client index.Client ) ( summary views.Summary ) {
	history := handlers.Domain.Closet.Index.VisitsOf( client.ID )
	count := 0
	for _ , entry := range history {
		if entry.Visit.Counts() {
			count++
		}
	}
	var last *visits.Entry
	if found , ok := visits.LastVisit( history ); ok {
		last = &found
	}
	summary = views.NewSummary( client , last , count , handlers.Domain.Closet.Location() )
	return
}

// RecentClients is the families who visited most recently (?limit=, 25).
func ( handlers *Handlers ) RecentClients( c fiber.Ctx ) ( err error ) {
	limit , _ := strconv.Atoi( c.Query( "limit" , "25" ) )
	if limit < 1 || limit > 200 {
		limit = 25
	}
	location := handlers.Domain.Closet.Location()
	rows := []views.Summary{}
	for _ , row := range handlers.Domain.Closet.Index.Recent( limit ) {
		last := row.Last
		rows = append( rows , views.NewSummary( row.Client , &last , row.Count , location ) )
	}
	err = c.JSON( fiber.Map{ "result": rows } )
	return
}

// SearchClients is the type-ahead name search (?q=).
func ( handlers *Handlers ) SearchClients( c fiber.Ctx ) ( err error ) {
	rows := []views.Summary{}
	query := strings.TrimSpace( c.Query( "q" ) )
	if len( query ) > 0 && len( query ) <= 100 {
		for _ , client := range handlers.Domain.Closet.Index.Search( query , 25 ) {
			rows = append( rows , handlers.summary( client ) )
		}
	}
	err = c.JSON( fiber.Map{ "result": rows } )
	return
}

// ClientByBarcode answers a scanned card. Two families sharing one number
// is possible (cards typed in by hand); the first is returned, flagged, so
// the volunteer can see something is off.
func ( handlers *Handlers ) ClientByBarcode( c fiber.Ctx ) ( err error ) {
	matches := handlers.Domain.Closet.Index.ByBarcode( strings.TrimSpace( c.Query( "code" ) ) )
	if len( matches ) == 0 {
		err = notFound( c , "unknown barcode" )
		return
	}
	view := handlers.userView( matches[ 0 ] )
	if len( matches ) > 1 {
		view[ "duplicate_barcode" ] = len( matches )
	}
	err = c.JSON( view )
	return
}

func ( handlers *Handlers ) GetClient( c fiber.Ctx ) ( err error ) {
	client , ok := handlers.Domain.Closet.Index.Client( param( c , "client_id" ) )
	if ok == false {
		err = notFound( c , "unknown user" )
		return
	}
	err = c.JSON( handlers.userView( client ) )
	return
}

func decodeClient( c fiber.Ctx ) ( client clients.Client , rev hlc.Stamp , err error ) {
	body := c.Body()
	if err = json.Unmarshal( body , &client ); err != nil { return }
	var meta struct {
		Rev string `json:"_rev"`
	}
	json.Unmarshal( body , &meta )
	rev , err = hlc.Parse( meta.Rev )
	return
}

// CreateClient registers a family from the new-family form.
func ( handlers *Handlers ) CreateClient( c fiber.Ctx ) ( err error ) {
	input , _ , decode_err := decodeClient( c )
	if decode_err != nil {
		err = badRequest( c , "malformed client" )
		return
	}
	created , create_err := handlers.Domain.Closet.CreateClient( input )
	if create_err != nil {
		err = closetError( c , create_err )
		return
	}
	err = c.JSON( fiber.Map{ "result": handlers.userView( created ) } )
	return
}

// UpdateClient saves an edited record. The page sends back the whole record
// it loaded, including its _rev; a record changed elsewhere since gets 409.
func ( handlers *Handlers ) UpdateClient( c fiber.Ctx ) ( err error ) {
	input , rev , decode_err := decodeClient( c )
	if decode_err != nil {
		err = badRequest( c , "malformed client" )
		return
	}
	updated , update_err := handlers.Domain.Closet.UpdateClient( param( c , "client_id" ) , input , rev )
	if update_err != nil {
		err = closetError( c , update_err )
		return
	}
	err = c.JSON( fiber.Map{ "result": true , "user": handlers.userView( updated ) } )
	return
}

// DeleteClient is admin-only.
func ( handlers *Handlers ) DeleteClient( c fiber.Ctx ) ( err error ) {
	if delete_err := handlers.Domain.Closet.DeleteClient( param( c , "client_id" ) ); delete_err != nil {
		err = closetError( c , delete_err )
		return
	}
	err = c.JSON( fiber.Map{ "ok": true } )
	return
}

// SimilarClients lists families that look like the same one.
func ( handlers *Handlers ) SimilarClients( c fiber.Ctx ) ( err error ) {
	matches , similar_err := handlers.Domain.Closet.Similar( param( c , "client_id" ) )
	if similar_err != nil {
		err = closetError( c , similar_err )
		return
	}
	rows := []views.Summary{}
	for _ , match := range matches {
		rows = append( rows , handlers.summary( match ) )
	}
	err = c.JSON( fiber.Map{ "result": rows } )
	return
}
