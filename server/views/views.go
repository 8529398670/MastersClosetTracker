// Package views is the only place that knows what JSON the current pages
// expect. The stored records are shaped for the backend; the pages were
// written against the old system's shapes (a "user" with embedded check-ins
// and a balance, dates like 27JUN2026, an upper-case "ULID" key). Everything
// that translates between the two is here, so when the pages are rewritten
// this package is what changes -- or goes.
package views

import (
	json "encoding/json"
	strings "strings"
	time "time"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	closet "github.com/0187773933/MastersClosetTracker/server/closet"
	index "github.com/0187773933/MastersClosetTracker/server/index"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

// labelLayout is the day format the pages show and put in URLs.
const labelLayout = "02Jan2006"

// DayLabel turns a stored day (2026-06-27) into the pages' form (27JUN2026).
func DayLabel( day string ) ( label string ) {
	parsed , err := time.Parse( visits.DayLayout , day )
	if err != nil {
		label = day
		return
	}
	label = strings.ToUpper( parsed.Format( labelLayout ) )
	return
}

// ParseDay accepts either form back from a URL. (Go matches month names
// without regard to case, so 27JUN2026 parses as 27Jun2026.)
func ParseDay( text string ) ( day string , ok bool ) {
	text = strings.TrimSpace( text )
	for _ , layout := range []string{ visits.DayLayout , labelLayout } {
		if parsed , err := time.Parse( layout , text ); err == nil {
			day , ok = parsed.Format( visits.DayLayout ) , true
			return
		}
	}
	return
}

func dateText( at time.Time , location *time.Location ) ( result string ) {
	if at.IsZero() { return }
	result = strings.ToUpper( at.In( location ).Format( labelLayout ) )
	return
}

func timeText( at time.Time , location *time.Location ) ( result string ) {
	if at.IsZero() { return }
	result = at.In( location ).Format( "15:04:05.000" )
	return
}

// ---------------------------------------------------------------------------
// check-ins
// ---------------------------------------------------------------------------

type Shopping struct {
	ShoppingFor int `json:"shopping_for"`
	Tops        int `json:"tops"`
	Bottoms     int `json:"bottoms"`
	Dresses     int `json:"dresses"`
	Shoes       int `json:"shoes"`
	Seasonals   int `json:"seasonals"`
	Accessories int `json:"accessories"`
	Boys        int `json:"boys"`
	Girls       int `json:"girls"`
	Men         int `json:"men"`
	Women       int `json:"women"`
	Guests      int `json:"guests"`
}

// CheckIn is one visit as the pages read it.
type CheckIn struct {
	UUID          string   `json:"uuid"`
	Name          string   `json:"name"`
	ULID          string   `json:"ULID"`
	Date          string   `json:"date"`
	Time          string   `json:"time"`
	Type          string   `json:"type"`
	Result        bool     `json:"result"`
	TimeRemaining int64    `json:"time_remaining"`
	Shopping      Shopping `json:"shopping"`
	Station       string   `json:"station,omitempty"`
	// FirstVisit marks the family's first visit ever (set by the day view).
	FirstVisit bool `json:"first_visit,omitempty"`
}

func NewCheckIn( entry visits.Entry , location *time.Location ) ( view CheckIn ) {
	visit := entry.Visit
	kind := visit.Kind
	if visit.LegacyType != "" {
		kind = visit.LegacyType
	}
	view = CheckIn{
		UUID:          visit.ClientID,
		Name:          visit.Name,
		ULID:          entry.ID,
		Date:          DayLabel( visit.Day ),
		Time:          timeText( visit.At , location ),
		Type:          kind,
		Result:        visit.Counts(),
		TimeRemaining: visit.TimeRemainingMS,
		Station:       visit.Station,
		Shopping: Shopping{
			ShoppingFor: visit.ShoppingFor,
			Tops:        visit.Items[ visits.ItemTops ],
			Bottoms:     visit.Items[ visits.ItemBottoms ],
			Dresses:     visit.Items[ visits.ItemDresses ],
			Shoes:       visit.Items[ visits.ItemShoes ],
			Seasonals:   visit.Items[ visits.ItemSeasonals ],
			Accessories: visit.Items[ visits.ItemAccessories ],
			Boys:        visit.People[ "boys" ],
			Girls:       visit.People[ "girls" ],
			Men:         visit.People[ "men" ],
			Women:       visit.People[ "women" ],
			Guests:      visit.Guests,
		},
	}
	return
}

func CheckIns( entries []visits.Entry , location *time.Location ) ( views []CheckIn ) {
	views = make( []CheckIn , 0 , len( entries ) )
	for _ , entry := range entries {
		views = append( views , NewCheckIn( entry , location ) )
	}
	return
}

// ---------------------------------------------------------------------------
// clients
// ---------------------------------------------------------------------------

// User is a client as the pages read it: the profile plus everything derived
// from visits. Pages send it back whole when saving; clients.Client drops
// every derived key on the way in.
func User( client index.Client , standing *closet.Standing , location *time.Location ) ( view map[string]any ) {
	raw , _ := json.Marshal( client.Record )
	json.Unmarshal( raw , &view )
	record := client.Record
	view[ "uuid" ] = client.ID
	view[ "_rev" ] = client.HLC.String()
	view[ "username" ] = record.Username()
	view[ "name_string" ] = record.Name()
	view[ "search_string" ] = strings.ToLower( record.Name() )
	view[ "spanish" ] = record.Language == "es"
	view[ "created_date" ] = dateText( record.CreatedAt , location )
	view[ "created_time" ] = timeText( record.CreatedAt , location )
	view[ "modified_date" ] = dateText( client.HLC.Time() , location )
	view[ "modified_time" ] = timeText( client.HLC.Time() , location )
	delete( view , "created_at" )
	delete( view , "pass_epoch" )
	if record.Barcodes == nil {
		view[ "barcodes" ] = []string{}
	}
	if record.FamilyMembers == nil {
		view[ "family_members" ] = []clients.Person{}
	}
	if record.AuthorizedAliases == nil {
		view[ "authorized_aliases" ] = []clients.Person{}
	}

	check_ins := []CheckIn{}
	failed := []CheckIn{}
	guests := 0
	if standing != nil {
		for _ , entry := range standing.History {
			if entry.Visit.Counts() {
				check_ins = append( check_ins , NewCheckIn( entry , location ) )
				guests += entry.Visit.Guests
			} else {
				failed = append( failed , NewCheckIn( entry , location ) )
			}
		}
		view[ "balance" ] = standing.Balance
		view[ "allowed_to_checkin" ] = standing.Outcome.Allowed
		view[ "time_remaining" ] = standing.Outcome.TimeRemainingMS
		if len( check_ins ) > 0 {
			last := check_ins[ len( check_ins )-1 ]
			view[ "last_check_in_date" ] = last.Date
			view[ "last_check_in_time" ] = last.Time
		}
	}
	view[ "check_ins" ] = check_ins
	view[ "failed_check_ins" ] = failed
	view[ "total_guests_admitted" ] = guests
	view[ "check_in_count" ] = len( check_ins )
	return
}

// Summary is one row of a client list.
type Summary struct {
	UUID            string   `json:"uuid"`
	Username        string   `json:"username"`
	NameString      string   `json:"name_string"`
	Barcodes        []string `json:"barcodes"`
	Verified        bool     `json:"verified"`
	LastCheckInDate string   `json:"last_check_in_date"`
	LastCheckInTime string   `json:"last_check_in_time,omitempty"`
	CheckInCount    int      `json:"check_in_count"`
}

func NewSummary( client index.Client , last *visits.Entry , count int , location *time.Location ) ( summary Summary ) {
	summary = Summary{
		UUID:         client.ID,
		Username:     client.Record.Username(),
		NameString:   client.Record.Name(),
		Barcodes:     client.Record.Barcodes,
		Verified:     client.Record.Verified,
		CheckInCount: count,
	}
	if summary.Barcodes == nil {
		summary.Barcodes = []string{}
	}
	if last != nil {
		summary.LastCheckInDate = DayLabel( last.Visit.Day )
		summary.LastCheckInTime = timeText( last.Visit.At , location )
	}
	return
}

// ---------------------------------------------------------------------------
// collection days
// ---------------------------------------------------------------------------

type Day struct {
	Date             string    `json:"date"`
	Count            int       `json:"count"`
	ShoppedFor       int       `json:"shopped_for"`
	ManualShoppedFor int       `json:"manual_shopped_for"`
	CheckIns         []CheckIn `json:"check_ins"`
}

func Days( summaries []visits.DaySummary ) ( days []Day ) {
	days = make( []Day , 0 , len( summaries ) )
	for _ , summary := range summaries {
		days = append( days , Day{
			Date:             DayLabel( summary.Day ),
			Count:            summary.Count,
			ShoppedFor:       summary.ShoppedFor,
			ManualShoppedFor: summary.ManualShoppedFor,
		} )
	}
	return
}

type Manual struct {
	Date       string `json:"date"`
	ShoppedFor int    `json:"shopped_for"`
	Note       string `json:"note"`
}

func NewManual( day string , tally visits.Tally ) ( view Manual ) {
	view = Manual{ Date: DayLabel( day ) , ShoppedFor: tally.ShoppedFor , Note: tally.Note }
	return
}
