package legacy

import (
	json "encoding/json"
	strconv "strconv"
	strings "strings"
	time "time"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

// The old records are read field by field from loose JSON rather than into
// fixed structs: across v1 and v2 a field was sometimes absent, null, or a
// different type, and a strict decode would drop the whole record for it.

type loose map[string]any

func ( object loose ) text( name string ) ( result string ) {
	switch value := object[ name ].( type ) {
	case string:
		result = strings.TrimSpace( value )
	case float64:
		result = strconv.FormatFloat( value , 'f' , -1 , 64 )
	}
	return
}

func ( object loose ) number( name string ) ( result int ) {
	switch value := object[ name ].( type ) {
	case float64:
		result = int( value )
	case string:
		parsed , _ := strconv.ParseFloat( strings.TrimSpace( value ) , 64 )
		result = int( parsed )
	}
	return
}

func ( object loose ) flag( name string ) ( result bool ) {
	switch value := object[ name ].( type ) {
	case bool:
		result = value
	case string:
		result , _ = strconv.ParseBool( value )
	}
	return
}

func ( object loose ) child( name string ) ( result loose ) {
	result , _ = object[ name ].( map[string]any )
	if result == nil {
		result = loose{}
	}
	return
}

func ( object loose ) list( name string ) ( result []loose ) {
	items , _ := object[ name ].( []any )
	for _ , item := range items {
		if mapped , ok := item.( map[string]any ); ok {
			result = append( result , loose( mapped ) )
		}
	}
	return
}

func ( object loose ) strings( name string ) ( result []string ) {
	items , _ := object[ name ].( []any )
	for _ , item := range items {
		if text , ok := item.( string ); ok && strings.TrimSpace( text ) != "" {
			result = append( result , strings.TrimSpace( text ) )
		}
	}
	return
}

type oldCheckIn struct {
	ULID          string
	Date          string
	Time          string
	Type          string
	Name          string
	TimeRemaining int
	Shopping      loose
	PrintJob      loose
}

type oldUser struct {
	UUID           string
	Record         loose
	CheckIns       []oldCheckIn
	FailedCheckIns []oldCheckIn
}

func parseCheckIns( items []loose ) ( result []oldCheckIn ) {
	for _ , item := range items {
		result = append( result , oldCheckIn{
			ULID:          item.text( "ULID" ),
			Date:          item.text( "date" ),
			Time:          item.text( "time" ),
			Type:          item.text( "type" ),
			Name:          item.text( "name" ),
			TimeRemaining: item.number( "time_remaining" ),
			Shopping:      item.child( "shopping" ),
			PrintJob:      item.child( "print_job" ),
		} )
	}
	return
}

func parseUser( plain []byte ) ( user oldUser , err error ) {
	var record loose
	if err = json.Unmarshal( plain , &record ); err != nil { return }
	user = oldUser{
		UUID:           record.text( "uuid" ),
		Record:         record,
		CheckIns:       parseCheckIns( record.list( "check_ins" ) ),
		FailedCheckIns: parseCheckIns( record.list( "failed_check_ins" ) ),
	}
	return
}

func parsePerson( object loose ) ( person clients.Person ) {
	address := object.child( "address" )
	birthday := object.child( "date_of_birth" )
	person = clients.Person{
		FirstName:  object.text( "first_name" ),
		MiddleName: object.text( "middle_name" ),
		LastName:   object.text( "last_name" ),
		Address: clients.Address{
			StreetNumber: address.text( "street_number" ),
			StreetName:   address.text( "street_name" ),
			AddressTwo:   address.text( "address_two" ),
			City:         address.text( "city" ),
			State:        address.text( "state" ),
			ZipCode:      address.text( "zipcode" ),
		},
		DateOfBirth: clients.DateOfBirth{
			Month: birthday.text( "month" ),
			Day:   birthday.number( "day" ),
			Year:  birthday.number( "year" ),
		},
		Age:      object.number( "age" ),
		Sex:      object.text( "sex" ),
		Height:   object.text( "height" ),
		EyeColor: object.text( "eye_color" ),
		Spouse:   object.flag( "spouse" ),
	}
	return
}

// oldMoment reads the old "02JAN2006" + "15:04:05.000" pair in the closet's
// time zone.
func oldMoment( date string , clock string , location *time.Location ) ( moment time.Time , ok bool ) {
	if clock == "" {
		clock = "00:00:00.000"
	}
	for _ , layout := range []string{ "02Jan2006 15:04:05.000" , "02Jan2006 15:04:05" } {
		if parsed , err := time.ParseInLocation( layout , date+" "+clock , location ); err == nil {
			moment , ok = parsed , true
			return
		}
	}
	if parsed , err := time.ParseInLocation( "02Jan2006" , date , location ); err == nil {
		moment , ok = parsed , true
	}
	return
}

func convertClient( user oldUser , location *time.Location ) ( client clients.Client , stamp hlc.Stamp ) {
	record := user.Record
	client = clients.Client{
		Verified:     record.flag( "verified" ),
		Barcodes:     record.strings( "barcodes" ),
		EmailAddress: record.text( "email_address" ),
		PhoneNumber:  record.text( "phone_number" ),
		Identity:     parsePerson( record.child( "identity" ) ),
		FamilySize:   record.number( "family_size" ),
		Language:     strings.ToLower( record.text( "language" ) ),
	}
	for _ , member := range record.list( "family_members" ) {
		client.FamilyMembers = append( client.FamilyMembers , parsePerson( member ) )
	}
	for _ , alias := range record.list( "authorized_aliases" ) {
		client.AuthorizedAliases = append( client.AuthorizedAliases , parsePerson( alias ) )
	}
	if client.Language == "" && record.flag( "spanish" ) {
		client.Language = "es"
	}
	if created , ok := oldMoment( record.text( "created_date" ) , record.text( "created_time" ) , location ); ok {
		client.CreatedAt = created.UTC()
	}
	clients.Normalize( &client )

	// The profile's stamp is when it was last edited, so of two old
	// databases holding the same family, the more recently edited profile
	// wins -- and any edit made after the import wins over both.
	modified , ok := oldMoment( record.text( "modified_date" ) , record.text( "modified_time" ) , location )
	if ok == false {
		modified = client.CreatedAt
	}
	stamp = hlc.FromTime( modified )
	return
}

// convertVisit turns one old check-in into a visit. The people count is
// settled the way the old reports counted it (the ticket's own count, else
// the old printed family size, else the family's size, else one), so day
// totals match what the old system showed.
func convertVisit( client_id string , user oldUser , check_in oldCheckIn , refused bool , location *time.Location ) ( visit visits.Visit , ok bool ) {
	moment , ok := oldMoment( check_in.Date , check_in.Time , location )
	if ok == false { return }
	shopping , print_job := check_in.Shopping , check_in.PrintJob
	people := shopping.number( "shopping_for" )
	if people <= 0 {
		people = print_job.number( "family_size" )
	}
	if people <= 0 {
		people = user.Record.number( "family_size" )
	}
	if people <= 0 {
		people = 1
	}
	pick := func( name string , fallback string ) ( result int ) {
		result = shopping.number( name )
		if result == 0 && fallback != "" {
			result = print_job.number( fallback )
		}
		return
	}
	kind := visits.KindNormal
	switch {
	case refused:
		kind = visits.KindDenied
	case check_in.Type == "forced":
		kind = visits.KindForced
	}
	name := check_in.Name
	if name == "" {
		name = strings.TrimSpace( user.Record.text( "name_string" ) )
	}
	items := map[string]int{
		visits.ItemTops:        pick( "tops" , "total_clothing_items" ),
		visits.ItemBottoms:     shopping.number( "bottoms" ),
		visits.ItemDresses:     shopping.number( "dresses" ),
		visits.ItemShoes:       pick( "shoes" , "shoes" ),
		visits.ItemSeasonals:   pick( "seasonals" , "seasonal" ),
		visits.ItemAccessories: pick( "accessories" , "accessories" ),
	}
	if items[ visits.ItemBottoms ] == 0 {
		items[ visits.ItemBottoms ] = print_job.number( "pants_limit" ) * people
	}
	visit = visits.Visit{
		ClientID:    client_id,
		Name:        name,
		At:          moment.UTC(),
		Day:         moment.Format( visits.DayLayout ),
		Kind:        kind,
		ShoppingFor: people,
		Items:       items,
		People: map[string]int{
			"boys":  pick( "boys" , "boys" ),
			"girls": pick( "girls" , "girls" ),
			"men":   pick( "men" , "men" ),
			"women": pick( "women" , "women" ),
		},
		Guests:          pick( "guests" , "guests" ),
		LegacyType:      check_in.Type,
		TimeRemainingMS: int64( check_in.TimeRemaining ),
	}
	if visit.LegacyType == "" {
		visit.LegacyType = "unknown"
	}
	if len( print_job ) > 0 {
		language := ""
		if print_job.flag( "spanish" ) {
			language = "es"
		}
		clothing := 6
		if family := print_job.number( "family_size" ); family > 0 && print_job.number( "total_clothing_items" ) > 0 {
			clothing = print_job.number( "total_clothing_items" ) / family
		}
		visit.Ticket = &visits.Ticket{
			ClothingPerPerson: clothing,
			PantsLimit:        print_job.number( "pants_limit" ),
			ShoesLimit:        print_job.number( "shoes_limit" ),
			AccessoriesLimit:  print_job.number( "accessories_limit" ),
			SeasonalLimit:     print_job.number( "seasonal_limit" ),
			FamilyName:        print_job.text( "family_name" ),
			Language:          language,
			Barcode:           print_job.text( "barcode_number" ),
		}
	}
	return
}
