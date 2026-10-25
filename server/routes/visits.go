package routes

import (
	errors "errors"
	fmt "fmt"
	log "log"

	fiber "github.com/gofiber/fiber/v3"

	closet "github.com/0187773933/MastersClosetTracker/server/closet"
	export "github.com/0187773933/MastersClosetTracker/server/export"
	printer "github.com/0187773933/MastersClosetTracker/server/printer"
	views "github.com/0187773933/MastersClosetTracker/server/views"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

// Check-ins: the check-in screen, recording a visit (and printing its
// ticket), the history by day, the paper-ticket tally, and the exports.

// CheckInScreen is everything the check-in page shows before committing:
// whether the family may come yet, the family, and the default allowances.
func ( handlers *Handlers ) CheckInScreen( c fiber.Ctx ) ( err error ) {
	standing , standing_err := handlers.Domain.Closet.Standing( param( c , "client_id" ) )
	if standing_err != nil {
		err = closetError( c , standing_err )
		return
	}
	location := handlers.Domain.Closet.Location()
	err = c.JSON( fiber.Map{
		"result":         standing.Outcome,
		"user":           views.User( standing.Client , &standing , location ),
		"balance_config": handlers.Domain.Settings.Org().Balance,
		"handoff_qr":     handlers.Domain.Settings.Org().HandoffQR && handlers.Config.SitePublicURL != "",
	} )
	return
}

// ticketJob is what the printer draws for a visit -- from the ticket that
// was printed at the time, so a reprint is identical.
func ticketJob( visit visits.Visit ) ( job printer.Job ) {
	ticket := visits.Ticket{ ClothingPerPerson: 6 , FamilyName: visit.Name }
	if visit.Ticket != nil {
		ticket = *visit.Ticket
	}
	family_name := ticket.FamilyName
	if family_name == "" {
		family_name = visit.Name
	}
	job = printer.Job{
		FamilySize:         visit.ShoppingFor,
		TotalClothingItems: ticket.ClothingPerPerson * visit.ShoppingFor,
		ClothingPerPerson:  ticket.ClothingPerPerson,
		PantsLimit:         ticket.PantsLimit,
		ShoesLimit:         ticket.ShoesLimit,
		AccessoriesLimit:   ticket.AccessoriesLimit,
		SeasonalLimit:      ticket.SeasonalLimit,
		FamilyName:         family_name,
		BarcodeNumber:      ticket.Barcode,
		Language:           ticket.Language,
		Guests:             visit.Guests,
		Boys:               visit.People[ "boys" ],
		Girls:              visit.People[ "girls" ],
		Men:                visit.People[ "men" ],
		Women:              visit.People[ "women" ],
	}
	// Guests printing their own tickets are not also counted on the family's.
	if ticket.PrintGuestTickets {
		job.Guests = 0
	}
	return
}

// withPass adds the phone pass QR code to a ticket when the closet prints
// one: a link to the public site carrying the ticket's own barcode.
func ( handlers *Handlers ) withPass( job printer.Job ) ( result printer.Job ) {
	result = job
	if handlers.Domain.Settings.Org().HandoffQR {
		result.PassURL = handlers.passURL( job.BarcodeNumber )
	}
	return
}

// printTickets prints a visit's family ticket and, when asked for, each
// guest's own ticket. A print failure is reported, never fatal: the visit is
// already recorded either way.
func ( handlers *Handlers ) printTickets( entry visits.Entry ) ( printed bool , print_error string , guests_printed int ) {
	printer_settings := handlers.Domain.Settings.Printer()
	job := handlers.withPass( ticketJob( entry.Visit ) )
	// Nobody in the family shopping (only guests) means no family ticket.
	if entry.Visit.ShoppingFor >= 1 {
		var print_err error
		if printed , print_err = printer.Print( printer_settings , handlers.Config.AppDir , job ); print_err != nil {
			print_error = print_err.Error()
			log.Printf( "[printer] check-in ticket failed: %v" , print_err )
		}
	}
	// Opt-in: each guest also gets their own one-person ticket, carrying the
	// family's barcode.
	if entry.Visit.Ticket != nil && entry.Visit.Ticket.PrintGuestTickets {
		for guest := 1; guest <= entry.Visit.Guests; guest++ {
			guest_job := job
			guest_job.GuestNumber = guest
			guest_job.FamilySize = 1
			guest_job.TotalClothingItems = job.ClothingPerPerson
			guest_job.Guests = 0
			guest_printed , guest_err := printer.Print( printer_settings , handlers.Config.AppDir , guest_job )
			if guest_err != nil {
				if print_error == "" {
					print_error = guest_err.Error()
				}
				log.Printf( "[printer] guest ticket failed: %v" , guest_err )
				continue
			}
			if guest_printed {
				guests_printed++
			}
		}
	}
	return
}

// RecordVisit records the visit and prints the ticket exactly as it was
// previewed. A print failure never undoes the visit: it is recorded, and the
// ticket can be reprinted from the history.
func ( handlers *Handlers ) RecordVisit( c fiber.Ctx ) ( err error ) {
	var request visits.ShoppingRequest
	if c.Bind().JSON( &request ) != nil {
		err = badRequest( c , "invalid ticket" )
		return
	}
	client_id := param( c , "client_id" )
	entry , _ , record_err := handlers.Domain.Closet.RecordShopping( client_id , request )
	if errors.Is( record_err , closet.ErrRecentVisit ) {
		// Name the visit that was just made, so the page can offer to
		// reprint its ticket instead of recording a second one.
		response := fiber.Map{ "error": record_err.Error() , "result": false , "recent": true }
		if last , found := visits.LastVisit( handlers.Domain.Closet.Index.VisitsOf( client_id ) ); found {
			response[ "recent_visit_id" ] = last.ID
		}
		err = c.Status( fiber.StatusConflict ).JSON( response )
		return
	}
	if record_err != nil {
		err = closetError( c , record_err )
		return
	}
	printed , print_error , guests_printed := handlers.printTickets( entry )
	err = c.JSON( fiber.Map{
		"result":         true,
		"check_in":       views.NewCheckIn( entry , handlers.Domain.Closet.Location() ),
		"printed":        printed,
		"print_error":    print_error,
		"guests_printed": guests_printed,
	} )
	return
}

// ReprintVisit prints a recorded visit's tickets again -- a jammed label, a
// lost ticket -- without recording another visit.
func ( handlers *Handlers ) ReprintVisit( c fiber.Ctx ) ( err error ) {
	entry , ok := handlers.Domain.Closet.Index.Visit( param( c , "visit_id" ) )
	if ok == false {
		err = notFound( c , "unknown check-in" )
		return
	}
	printed , print_error , guests_printed := handlers.printTickets( entry )
	err = c.JSON( fiber.Map{
		"result":         true,
		"check_in":       views.NewCheckIn( entry , handlers.Domain.Closet.Location() ),
		"printed":        printed,
		"print_error":    print_error,
		"guests_printed": guests_printed,
	} )
	return
}

// TicketPDF renders a recorded visit's ticket for viewing or reprinting.
func ( handlers *Handlers ) TicketPDF( c fiber.Ctx ) ( err error ) {
	entry , ok := handlers.Domain.Closet.Index.Visit( param( c , "visit_id" ) )
	if ok == false {
		err = notFound( c , "unknown check-in" )
		return
	}
	job := handlers.withPass( ticketJob( entry.Visit ) )
	content , render_err := printer.Render( handlers.Domain.Settings.Printer() , job )
	if render_err != nil {
		log.Printf( "[printer] render failed: %v" , render_err )
		err = serverError( c )
		return
	}
	// The browser's own PDF viewer is a plugin, which the pages' policy
	// (object-src 'none') would refuse to run.
	c.Response().Header.Del( fiber.HeaderContentSecurityPolicy )
	c.Set( fiber.HeaderContentType , "application/pdf" )
	c.Set( fiber.HeaderContentDisposition , fmt.Sprintf( `inline; filename="ticket-%s.pdf"` , entry.ID ) )
	err = c.Send( content )
	return
}

// VisitDays is one row per collection day, newest first.
func ( handlers *Handlers ) VisitDays( c fiber.Ctx ) ( err error ) {
	err = c.JSON( fiber.Map{ "result": views.Days( handlers.Domain.Closet.Index.Days() ) } )
	return
}

func ( handlers *Handlers ) dayParam( c fiber.Ctx ) ( day string , ok bool ) {
	day , ok = views.ParseDay( param( c , "day" ) )
	return
}

// VisitDay is every visit on one day, oldest first, with the day's tally.
func ( handlers *Handlers ) VisitDay( c fiber.Ctx ) ( err error ) {
	day , ok := handlers.dayParam( c )
	if ok == false {
		err = badRequest( c , "invalid date" )
		return
	}
	index := handlers.Domain.Closet.Index
	entries := index.Day( day )
	check_ins := views.CheckIns( entries , handlers.Domain.Closet.Location() )
	// Flag the families who were here for the first time.
	client_ids := make( []string , 0 , len( entries ) )
	for _ , entry := range entries {
		client_ids = append( client_ids , entry.Visit.ClientID )
	}
	firsts := index.FirstVisits( client_ids )
	for i , entry := range entries {
		check_ins[ i ].FirstVisit = firsts[ entry.Visit.ClientID ] == entry.ID
	}
	err = c.JSON( fiber.Map{
		"date":   views.DayLabel( day ),
		"result": check_ins,
		"manual": views.NewManual( day , index.Tally( day ) ),
	} )
	return
}

type tallyRequest struct {
	ShoppedFor *int    `json:"shopped_for"`
	Note       *string `json:"note"`
}

// SetDayTally records how many people were served on hand-written tickets.
func ( handlers *Handlers ) SetDayTally( c fiber.Ctx ) ( err error ) {
	day , ok := handlers.dayParam( c )
	if ok == false {
		err = badRequest( c , "invalid date" )
		return
	}
	var body tallyRequest
	if c.Bind().JSON( &body ) != nil {
		err = badRequest( c , "invalid body" )
		return
	}
	tally , tally_err := handlers.Domain.Closet.SetTally( day , body.ShoppedFor , body.Note )
	if tally_err != nil {
		err = closetError( c , tally_err )
		return
	}
	err = c.JSON( fiber.Map{ "result": true , "manual": views.NewManual( day , tally ) } )
	return
}

// DeleteVisit is admin-only.
func ( handlers *Handlers ) DeleteVisit( c fiber.Ctx ) ( err error ) {
	if delete_err := handlers.Domain.Closet.DeleteVisit( param( c , "visit_id" ) ); delete_err != nil {
		err = closetError( c , delete_err )
		return
	}
	err = c.JSON( fiber.Map{ "result": "deleted" } )
	return
}

func ( handlers *Handlers ) sendWorkbook( c fiber.Ctx , days []export.Day , filename string ) ( err error ) {
	content , build_err := export.Workbook( days , views.DayLabel , handlers.Domain.Closet.Location() , handlers.Domain.Closet.Now() )
	if build_err != nil {
		log.Printf( "[export] %v" , build_err )
		err = serverError( c )
		return
	}
	err = sendXLSX( c , content , filename )
	return
}

// sendXLSX sends a workbook as a download.
func sendXLSX( c fiber.Ctx , content []byte , filename string ) ( err error ) {
	c.Response().Header.Del( fiber.HeaderContentSecurityPolicy )
	c.Set( fiber.HeaderContentType , "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" )
	c.Set( fiber.HeaderContentDisposition , fmt.Sprintf( `attachment; filename="%s"` , filename ) )
	err = c.Send( content )
	return
}

// ExportAll is the whole history: a totals sheet, then a sheet per day.
func ( handlers *Handlers ) ExportAll( c fiber.Ctx ) ( err error ) {
	index := handlers.Domain.Closet.Index
	var days []export.Day
	for _ , summary := range index.Days() {
		days = append( days , export.Day{ Summary: summary , Entries: index.Day( summary.Day ) } )
	}
	err = handlers.sendWorkbook( c , days , fmt.Sprintf( "checkins_%s.xlsx" , handlers.Domain.Closet.Now().Format( "20060102" ) ) )
	return
}

// ExportDay is one day's report.
func ( handlers *Handlers ) ExportDay( c fiber.Ctx ) ( err error ) {
	day , ok := handlers.dayParam( c )
	if ok == false {
		err = badRequest( c , "invalid date" )
		return
	}
	index := handlers.Domain.Closet.Index
	entries := index.Day( day )
	summary := visits.DaySummary{ Day: day , Count: len( entries ) , ManualShoppedFor: index.Tally( day ).ShoppedFor }
	for _ , entry := range entries {
		summary.ShoppedFor += entry.Visit.ShoppingFor
	}
	err = handlers.sendWorkbook( c , []export.Day{ { Summary: summary , Entries: entries } } , fmt.Sprintf( "checkins_%s.xlsx" , views.DayLabel( day ) ) )
	return
}
