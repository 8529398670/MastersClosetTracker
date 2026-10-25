package routes

import (
	json "encoding/json"
	fmt "fmt"
	log "log"
	strconv "strconv"
	strings "strings"

	fiber "github.com/gofiber/fiber/v3"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	export "github.com/0187773933/MastersClosetTracker/server/export"
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
// ?day=today is instead every family whose last visit was today, however
// many.
func ( handlers *Handlers ) RecentClients( c fiber.Ctx ) ( err error ) {
	limit , _ := strconv.Atoi( c.Query( "limit" , "25" ) )
	if limit < 1 || limit > 200 {
		limit = 25
	}
	closet := handlers.Domain.Closet
	location := closet.Location()
	today := ""
	if c.Query( "day" ) == "today" {
		limit = 0
		today = visits.DayKey( closet.Now() , location )
	}
	rows := []views.Summary{}
	for _ , row := range closet.Index.Recent( limit ) {
		if today != "" && row.Last.Visit.Day != today { continue }
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

// ListBarcodes is every barcode on every family, newest assignment first.
func ( handlers *Handlers ) ListBarcodes( c fiber.Ctx ) ( err error ) {
	err = c.JSON( fiber.Map{ "result": handlers.Domain.Closet.BarcodeRows() } )
	return
}

// ExportBarcodes is the same list as a workbook.
func ( handlers *Handlers ) ExportBarcodes( c fiber.Ctx ) ( err error ) {
	closet := handlers.Domain.Closet
	now := closet.Now()
	content , build_err := export.Barcodes( closet.BarcodeRows() , closet.Location() , now )
	if build_err != nil {
		log.Printf( "[export] %v" , build_err )
		err = serverError( c )
		return
	}
	err = sendXLSX( c , content , fmt.Sprintf( "barcodes_%s.xlsx" , now.In( closet.Location() ).Format( "20060102" ) ) )
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

// DuplicateClients is every group of families that look like the same one.
func ( handlers *Handlers ) DuplicateClients( c fiber.Ctx ) ( err error ) {
	type member struct {
		views.Summary
		Email     string          `json:"email_address"`
		Phone     string          `json:"phone_number"`
		Address   clients.Address `json:"address"`
		CreatedAt string          `json:"created_at,omitempty"`
		Reasons   []string        `json:"reasons"`
		Rev       string          `json:"_rev"`
	}
	type group struct {
		Members []member `json:"members"`
	}
	result := []group{}
	for _ , found := range handlers.Domain.Closet.Duplicates() {
		row := group{}
		for _ , entry := range found.Members {
			record := entry.Client.Record
			created := ""
			if record.CreatedAt.IsZero() == false {
				created = record.CreatedAt.Format( "2006-01-02" )
			}
			row.Members = append( row.Members , member{
				Summary:   handlers.summary( entry.Client ),
				Email:     record.EmailAddress,
				Phone:     record.PhoneNumber,
				Address:   record.Identity.Address,
				CreatedAt: created,
				Reasons:   entry.Reasons,
				Rev:       entry.Client.HLC.String(),
			} )
		}
		result = append( result , row )
	}
	err = c.JSON( fiber.Map{ "result": result } )
	return
}

// MergeClients folds other families into one ({keep, others, _rev}); the
// others are deleted, their visits and barcodes moved to the one kept.
func ( handlers *Handlers ) MergeClients( c fiber.Ctx ) ( err error ) {
	var body struct {
		Keep   string   `json:"keep"`
		Others []string `json:"others"`
		Rev    string   `json:"_rev"`
	}
	if json.Unmarshal( c.Body() , &body ) != nil || body.Keep == "" || len( body.Others ) == 0 || len( body.Others ) > 50 {
		err = badRequest( c , "malformed merge" )
		return
	}
	rev , parse_err := hlc.Parse( body.Rev )
	if parse_err != nil {
		err = badRequest( c , "malformed merge" )
		return
	}
	merged , merge_err := handlers.Domain.Closet.MergeClients( body.Keep , body.Others , rev )
	if merge_err != nil {
		err = closetError( c , merge_err )
		return
	}
	err = c.JSON( fiber.Map{ "result": handlers.userView( merged ) } )
	return
}
