// Package visits is a family's trips to the closet and the rules that are
// computed from them: whether a family may come again yet (the cooloff), how
// much of their allowance is left (the balance), and how a collection day
// adds up.
//
// Each visit is its own record, written once by the station that recorded
// it. Nothing is ever stored that is derived from visits -- the balance and
// the day totals are worked out from the visits every time -- so two
// stations recording visits for the same family at once simply both count.
package visits

import (
	sort "sort"
	time "time"

	settings "github.com/0187773933/MastersClosetTracker/server/settings"
)

const (
	Collection      = "visits"
	TallyCollection = "day_tallies"

	// Kinds. normal and forced are visits; denied is a refused attempt,
	// kept for the record but counted toward nothing.
	KindNormal = "normal"
	KindForced = "forced"
	KindDenied = "denied"

	// DayLayout is how a day is stored and keyed.
	DayLayout = "2006-01-02"
)

// Item keys. The ticket's lines map onto these.
const (
	ItemTops        = "tops"
	ItemBottoms     = "bottoms"
	ItemDresses     = "dresses"
	ItemShoes       = "shoes"
	ItemSeasonals   = "seasonals"
	ItemAccessories = "accessories"
)

// Ticket is what was printed, so a reprint matches the original exactly
// instead of being rebuilt from today's settings.
type Ticket struct {
	ClothingPerPerson int    `json:"clothing_per_person"`
	PantsLimit        int    `json:"pants_limit"`
	ShoesLimit        int    `json:"shoes_limit"`
	AccessoriesLimit  int    `json:"accessories_limit"`
	SeasonalLimit     int    `json:"seasonal_limit"`
	FamilyName        string `json:"family_name"`
	Language          string `json:"language"`
	Barcode           string `json:"barcode"`
	PrintGuestTickets bool   `json:"print_guest_tickets"`
	// Again is the volunteer confirming a second check-in moments after the
	// first; it skips the just-checked-in guard and is not stored.
	Again bool `json:"again"`
}

// Visit is one stored visit. Name is the family's name when they came, so
// history reads right after a rename.
type Visit struct {
	ClientID    string         `json:"client_id"`
	Name        string         `json:"name"`
	At          time.Time      `json:"at"`
	Day         string         `json:"day"`
	Kind        string         `json:"kind"`
	ShoppingFor int            `json:"shopping_for"`
	Items       map[string]int `json:"items,omitempty"`
	People      map[string]int `json:"people,omitempty"`
	Guests      int            `json:"guests"`
	Ticket      *Ticket        `json:"ticket,omitempty"`
	Station     string         `json:"station,omitempty"`

	// For a denied attempt: how long the family still had to wait.
	TimeRemainingMS int64 `json:"time_remaining_ms,omitempty"`

	// LegacyType is the old system's kind for an imported visit ("first",
	// "new", "normal", ...). Present only on imports.
	LegacyType string `json:"legacy_type,omitempty"`
}

// Counts reports whether a visit counts as a visit (not a refused attempt).
func ( visit Visit ) Counts() ( result bool ) {
	result = visit.Kind != KindDenied
	return
}

// Entry is a visit together with its record id.
type Entry struct {
	ID    string
	Visit Visit
}

// DayKey is the day a moment falls on in the closet's time zone.
func DayKey( at time.Time , location *time.Location ) ( day string ) {
	day = at.In( location ).Format( DayLayout )
	return
}

// dayStart is midnight at the start of a stored day, in the closet's zone.
func dayStart( day string , location *time.Location ) ( start time.Time , ok bool ) {
	start , err := time.ParseInLocation( DayLayout , day , location )
	ok = err == nil
	return
}

// ---------------------------------------------------------------------------
// cooloff
// ---------------------------------------------------------------------------

// Outcome is whether a family may check in now.
type Outcome struct {
	Allowed         bool  `json:"allowed"`
	TimeRemainingMS int64 `json:"time_remaining_ms"`
	DaysRemaining   int   `json:"days_remaining"`
	FirstEver       bool  `json:"first_ever"`
}

// Evaluate is the cooloff rule: a family may come back once cooloff_days
// have passed since the start of the day of their last visit. Counting from
// the start of the day means coming at 4pm one month and 10am the next is
// still fine.
func Evaluate( history []Entry , now time.Time , cooloff_days int , location *time.Location ) ( outcome Outcome ) {
	last , found := LastVisit( history )
	if found == false {
		outcome = Outcome{ Allowed: true , FirstEver: true }
		return
	}
	start , ok := dayStart( last.Visit.Day , location )
	if ok == false {
		start = last.Visit.At
	}
	remaining := time.Duration( cooloff_days )*24*time.Hour - now.Sub( start )
	if remaining <= 0 {
		outcome.Allowed = true
		return
	}
	outcome.TimeRemainingMS = remaining.Milliseconds()
	outcome.DaysRemaining = int( remaining.Hours() / 24 )
	return
}

// LastVisit is the most recent visit that counts.
func LastVisit( history []Entry ) ( last Entry , found bool ) {
	for _ , entry := range history {
		if entry.Visit.Counts() == false { continue }
		if found == false || entry.Visit.At.After( last.Visit.At ) || ( entry.Visit.At.Equal( last.Visit.At ) && entry.ID > last.ID ) {
			last , found = entry , true
		}
	}
	return
}

// ---------------------------------------------------------------------------
// balance
// ---------------------------------------------------------------------------

// Line is one allowance category: what the family may take this period,
// what they have taken, and what is left.
type Line struct {
	Available int `json:"available"`
	Limit     int `json:"limit"`
	Used      int `json:"used"`
}

type GeneralLines struct {
	Total     int  `json:"total"`
	Available int  `json:"available"`
	Tops      Line `json:"tops"`
	Bottoms   Line `json:"bottoms"`
	Dresses   Line `json:"dresses"`
}

// Balance is in the shape the check-in page reads.
type Balance struct {
	General     GeneralLines `json:"general"`
	Shoes       Line         `json:"shoes"`
	Seasonals   Line         `json:"seasonals"`
	Accessories Line         `json:"accessories"`
}

// ComputeBalance works out what is left: each category's per-person
// allowance times the family size, less what the family took in visits
// inside the current cooloff window. Nothing is stored, so every station
// computes the same answer from the same visits.
func ComputeBalance( history []Entry , family_size int , allowance settings.Balance , now time.Time , cooloff_days int , location *time.Location ) ( balance Balance ) {
	if family_size < 1 {
		family_size = 1
	}
	window_start := now.Add( -time.Duration( cooloff_days ) * 24 * time.Hour )
	used := map[string]int{}
	for _ , entry := range history {
		if entry.Visit.Counts() == false { continue }
		start , ok := dayStart( entry.Visit.Day , location )
		if ok == false {
			start = entry.Visit.At
		}
		if start.Before( window_start ) { continue }
		for item , count := range entry.Visit.Items {
			used[ item ] += count
		}
	}
	line := func( per_person int , item string ) ( result Line ) {
		result.Limit = per_person * family_size
		result.Used = used[ item ]
		result.Available = max( 0 , result.Limit-result.Used )
		return
	}
	balance.General.Tops = line( allowance.General.Tops , ItemTops )
	balance.General.Bottoms = line( allowance.General.Bottoms , ItemBottoms )
	balance.General.Dresses = line( allowance.General.Dresses , ItemDresses )
	balance.General.Total = allowance.General.Total * family_size
	// "Tops" on a ticket is the total clothing count, so it is what draws
	// down the general allowance.
	balance.General.Available = max( 0 , balance.General.Total-used[ ItemTops ] )
	balance.Shoes = line( allowance.Shoes , ItemShoes )
	balance.Seasonals = line( allowance.Seasonals , ItemSeasonals )
	balance.Accessories = line( allowance.Accessories , ItemAccessories )
	return
}

// ---------------------------------------------------------------------------
// recording a shopping visit
// ---------------------------------------------------------------------------

// ShoppingRequest is what the check-in page submits: the ticket as edited on
// screen, plus who the family shopped for.
type ShoppingRequest struct {
	ShoppingFor       int    `json:"shopping_for"`
	ClothingPerPerson int    `json:"clothing_per_person"`
	PantsLimit        int    `json:"pants_limit"`
	ShoesLimit        int    `json:"shoes_limit"`
	AccessoriesLimit  int    `json:"accessories_limit"`
	SeasonalLimit     int    `json:"seasonal_limit"`
	FamilyName        string `json:"family_name"`
	Language          string `json:"language"`
	Boys              int    `json:"boys"`
	Girls             int    `json:"girls"`
	Men               int    `json:"men"`
	Women             int    `json:"women"`
	Guests            int    `json:"guests"`
	PrintGuestTickets bool   `json:"print_guest_tickets"`
	// Again is the volunteer confirming a second check-in moments after the
	// first; it skips the just-checked-in guard and is not stored.
	Again bool `json:"again"`
}

// Clean clamps a request to sane numbers. shopping_for may be 0 (only
// guests are shopping); a missing clothing count takes the default.
func ( request ShoppingRequest ) Clean( default_clothing int ) ( cleaned ShoppingRequest ) {
	cleaned = request
	clamp := func( value int ) ( result int ) {
		result = min( max( value , 0 ) , 1000 )
		return
	}
	cleaned.ShoppingFor = clamp( cleaned.ShoppingFor )
	if cleaned.ClothingPerPerson <= 0 {
		cleaned.ClothingPerPerson = default_clothing
	}
	cleaned.ClothingPerPerson = clamp( cleaned.ClothingPerPerson )
	cleaned.PantsLimit = clamp( cleaned.PantsLimit )
	cleaned.ShoesLimit = clamp( cleaned.ShoesLimit )
	cleaned.AccessoriesLimit = clamp( cleaned.AccessoriesLimit )
	cleaned.SeasonalLimit = clamp( cleaned.SeasonalLimit )
	cleaned.Boys = clamp( cleaned.Boys )
	cleaned.Girls = clamp( cleaned.Girls )
	cleaned.Men = clamp( cleaned.Men )
	cleaned.Women = clamp( cleaned.Women )
	cleaned.Guests = clamp( cleaned.Guests )
	return
}

// NewShoppingVisit builds the visit a ticket describes. The items are the
// ticket's per-person limits times the people shopping; the people counts
// and guests are descriptive and never touch the allowance.
func NewShoppingVisit( client_id string , name string , request ShoppingRequest , barcode string , at time.Time , location *time.Location , station string ) ( visit Visit ) {
	people := request.ShoppingFor
	visit = Visit{
		ClientID:    client_id,
		Name:        name,
		At:          at.UTC(),
		Day:         DayKey( at , location ),
		Kind:        KindNormal,
		ShoppingFor: people,
		Items: map[string]int{
			ItemTops:        request.ClothingPerPerson * people,
			ItemBottoms:     request.PantsLimit * people,
			ItemShoes:       request.ShoesLimit * people,
			ItemSeasonals:   request.SeasonalLimit * people,
			ItemAccessories: request.AccessoriesLimit * people,
		},
		People: map[string]int{
			"boys": request.Boys , "girls": request.Girls , "men": request.Men , "women": request.Women ,
		},
		Guests: request.Guests,
		Ticket: &Ticket{
			ClothingPerPerson: request.ClothingPerPerson,
			PantsLimit:        request.PantsLimit,
			ShoesLimit:        request.ShoesLimit,
			AccessoriesLimit:  request.AccessoriesLimit,
			SeasonalLimit:     request.SeasonalLimit,
			FamilyName:        request.FamilyName,
			Language:          request.Language,
			Barcode:           barcode,
			PrintGuestTickets: request.PrintGuestTickets,
		},
		Station: station,
	}
	return
}

// ---------------------------------------------------------------------------
// collection days
// ---------------------------------------------------------------------------

// Tally is the hand-written-ticket count for one day: people served on
// paper who never went through the app, so the day's total is honest.
type Tally struct {
	ShoppedFor int    `json:"shopped_for"`
	Note       string `json:"note"`
}

// DaySummary is one collection day.
type DaySummary struct {
	Day              string
	Count            int
	ShoppedFor       int
	ManualShoppedFor int
}

// Summarize groups visits by day, newest day first. A day that was served
// entirely on paper still appears, from its tally.
func Summarize( entries []Entry , tallies map[string]Tally ) ( days []DaySummary ) {
	by_day := map[string]*DaySummary{}
	get := func( day string ) ( summary *DaySummary ) {
		summary = by_day[ day ]
		if summary == nil {
			summary = &DaySummary{ Day: day }
			by_day[ day ] = summary
		}
		return
	}
	for _ , entry := range entries {
		if entry.Visit.Counts() == false { continue }
		summary := get( entry.Visit.Day )
		summary.Count++
		summary.ShoppedFor += entry.Visit.ShoppingFor
	}
	for day , tally := range tallies {
		if tally.ShoppedFor > 0 {
			get( day ).ManualShoppedFor = tally.ShoppedFor
		}
	}
	for _ , summary := range by_day {
		days = append( days , *summary )
	}
	sort.Slice( days , func( i , j int ) bool { return days[ i ].Day > days[ j ].Day } )
	return
}

// SortByTime orders a day's (or a family's) visits oldest first.
func SortByTime( entries []Entry ) {
	sort.SliceStable( entries , func( i , j int ) bool {
		if entries[ i ].Visit.At.Equal( entries[ j ].Visit.At ) {
			return entries[ i ].ID < entries[ j ].ID
		}
		return entries[ i ].Visit.At.Before( entries[ j ].Visit.At )
	} )
}
