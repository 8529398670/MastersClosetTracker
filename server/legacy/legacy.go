// Package legacy reads the old Master's Closet databases (v1 and v2) and
// turns them into this system's records, once, so a fresh install starts with
// every family and every visit already there.
//
// It opens the old file read-only, decrypts each user record with the old
// key, and converts it:
//
//   - the profile becomes a client (same id, so importing from two old
//     databases merges rather than duplicates, and the newest profile wins
//     by its modified time);
//   - each check-in becomes a visit (its ULID kept as the id, or a stable
//     hash for the few without one), with the old day string as its day and
//     the old ticket as its ticket;
//   - failed check-ins become refused attempts;
//   - hand-written day tallies and, on request, the business settings come
//     across too.
//
// Everything is stamped from the old timestamps, so the result is identical
// however many times it is imported, and anything edited after the import
// wins over it. Records that cannot be read are listed, never silently
// skipped. Old balances, username indexes and similar-user caches are
// dropped: they are all computed now.
package legacy

import (
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	os "os"
	sort "sort"
	strings "strings"
	time "time"

	bolt "go.etcd.io/bbolt"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	settings "github.com/0187773933/MastersClosetTracker/server/settings"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

// Node is the node id imported records carry, so they are recognisable in
// an export.
const Node = "import"

type Options struct {
	// DBPath is the old bolt file. It is opened read-only; a running old
	// server holds a lock on it, so stop it or pass a copy.
	DBPath string

	// Key is the old bolt_db_encryption_key (64 hex characters).
	Key string

	// Settings also imports the business settings from the old database's
	// stored config (v2) or from SettingsJSON (an old config.json, for v1).
	Settings     bool
	SettingsJSON string

	// TimeZone the old dates and times were written in.
	TimeZone string
}

// Converted is everything read from one old database, ready to apply.
type Converted struct {
	Records []syncproto.Envelope
	Report  Report
}

type Problem struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Report says what an import found and did. Names never appear in it;
// ClientIDs lists ids for follow-up.
type Report struct {
	Source string `json:"source"`

	Users          int       `json:"users"`
	Problems       []Problem `json:"problems"`
	Clients        int       `json:"clients"`
	DerivedBarcode int       `json:"derived_barcodes"`

	Visits          int            `json:"visits"`
	VisitsByType    map[string]int `json:"visits_by_type"`
	Refused         int            `json:"refused"`
	VisitsNoULID    int            `json:"visits_without_ulid"`
	SameDayRepeats  int            `json:"same_day_repeats"`
	Tallies         int            `json:"tallies"`
	Settings        []string       `json:"settings"`
	SharedBarcodes  map[string]int `json:"shared_barcodes"`

	// Filled in when the records are applied (or checked, in a dry run).
	Applied      int      `json:"applied"`
	AlreadyThere int      `json:"already_there"`
	NewClientIDs []string `json:"new_client_ids"`
	DryRun       bool     `json:"dry_run"`
}

// ---------------------------------------------------------------------------
// reading the old file
// ---------------------------------------------------------------------------

// Read converts an old database without writing anything anywhere.
func Read( options Options ) ( converted Converted , err error ) {
	location , err := time.LoadLocation( options.TimeZone )
	if err != nil || options.TimeZone == "" {
		location , _ = time.LoadLocation( "America/New_York" )
		err = nil
	}
	if _ , err = os.Stat( options.DBPath ); err != nil { return }
	old , err := bolt.Open( options.DBPath , 0o400 , &bolt.Options{ ReadOnly: true , Timeout: 2 * time.Second } )
	if err != nil {
		err = fmt.Errorf( "could not open %s read-only (is the old server still running? stop it or import a copy): %w" , options.DBPath , err )
		return
	}
	defer old.Close()

	report := &converted.Report
	report.Source = options.DBPath
	report.VisitsByType = map[string]int{}
	report.SharedBarcodes = map[string]int{}
	report.Problems = []Problem{}

	var raw_users [][ 2 ][]byte
	var raw_tallies [][ 2 ][]byte
	var raw_config []byte
	err = old.View( func( tx *bolt.Tx ) ( tx_err error ) {
		if users := tx.Bucket( []byte( "users" ) ); users != nil {
			users.ForEach( func( key , value []byte ) error {
				raw_users = append( raw_users , [ 2 ][]byte{ append( []byte( nil ) , key... ) , append( []byte( nil ) , value... ) } )
				return nil
			} )
		}
		if tallies := tx.Bucket( []byte( "checkin-day-manual" ) ); tallies != nil {
			tallies.ForEach( func( key , value []byte ) error {
				raw_tallies = append( raw_tallies , [ 2 ][]byte{ append( []byte( nil ) , key... ) , append( []byte( nil ) , value... ) } )
				return nil
			} )
		}
		if config := tx.Bucket( []byte( "config" ) ); config != nil {
			raw_config = append( []byte( nil ) , config.Get( []byte( "current" ) )... )
		}
		return
	} )
	if err != nil { return }
	if len( raw_users ) == 0 {
		err = errors.New( "no users bucket in that file -- is it a Master's Closet database?" )
		return
	}

	barcode_owners := map[string]map[string]bool{}
	taken := func( barcode string ) bool { return len( barcode_owners[ barcode ] ) > 0 }
	type pending struct {
		id     string
		client clients.Client
		stamp  hlc.Stamp
	}
	var converted_clients []pending
	for _ , pair := range raw_users {
		report.Users++
		key := string( pair[ 0 ] )
		plain , open_err := encryption.LegacyChaChaOpen( options.Key , pair[ 1 ] )
		if open_err != nil {
			report.Problems = append( report.Problems , Problem{ Key: key , Reason: "could not decrypt with that key" } )
			continue
		}
		user , parse_err := parseUser( plain )
		if parse_err != nil {
			report.Problems = append( report.Problems , Problem{ Key: key , Reason: "not readable as a user record: " + parse_err.Error() } )
			continue
		}
		id := strings.TrimSpace( user.UUID )
		if id == "" {
			id = key
		}
		if syncproto.ValidID( id ) == false {
			report.Problems = append( report.Problems , Problem{ Key: key , Reason: "id is not usable as a record id" } )
			continue
		}
		client , stamp := convertClient( user , location )
		converted_clients = append( converted_clients , pending{ id , client , stamp } )
		for _ , barcode := range client.Barcodes {
			if barcode_owners[ barcode ] == nil {
				barcode_owners[ barcode ] = map[string]bool{}
			}
			barcode_owners[ barcode ][ id ] = true
		}

		seen_days := map[string]int{}
		for index , check_in := range user.CheckIns {
			id_for_visit , has_ulid := visitID( id , check_in , index )
			if has_ulid == false {
				report.VisitsNoULID++
			}
			visit , ok := convertVisit( id , user , check_in , false , location )
			if ok == false {
				report.Problems = append( report.Problems , Problem{ Key: key + "/" + id_for_visit , Reason: "check-in has an unreadable date" } )
				continue
			}
			report.Visits++
			report.VisitsByType[ visit.LegacyType ]++
			seen_days[ visit.Day ]++
			if seen_days[ visit.Day ] == 2 {
				report.SameDayRepeats++
			}
			converted.Records = append( converted.Records , envelope( visits.Collection , id_for_visit , hlc.FromTime( visit.At ) , visit ) )
		}
		for index , check_in := range user.FailedCheckIns {
			id_for_visit , _ := visitID( id , check_in , 100000+index )
			visit , ok := convertVisit( id , user , check_in , true , location )
			if ok == false { continue }
			report.Refused++
			converted.Records = append( converted.Records , envelope( visits.Collection , id_for_visit , hlc.FromTime( visit.At ) , visit ) )
		}
	}

	// Barcodes last, once every real one is known: families with none get a
	// stable virtual one that collides with nobody's.
	for index := range converted_clients {
		item := &converted_clients[ index ]
		if len( item.client.Barcodes ) == 0 {
			barcode := clients.DerivedBarcode( item.id , taken )
			item.client.Barcodes = []string{ barcode }
			barcode_owners[ barcode ] = map[string]bool{ item.id: true }
			report.DerivedBarcode++
		}
		converted.Records = append( converted.Records , envelope( clients.Collection , item.id , item.stamp , item.client ) )
		report.Clients++
	}
	for barcode , owners := range barcode_owners {
		if len( owners ) > 1 {
			report.SharedBarcodes[ barcode ] = len( owners )
		}
	}

	for _ , pair := range raw_tallies {
		var old_tally struct {
			ShoppedFor int    `json:"shopped_for"`
			Note       string `json:"note"`
			Modified   string `json:"modified"`
		}
		if json.Unmarshal( pair[ 1 ] , &old_tally ) != nil { continue }
		day , parse_err := time.ParseInLocation( "02Jan2006" , string( pair[ 0 ] ) , location )
		if parse_err != nil { continue }
		stamp := hlc.FromTime( day )
		if modified , modified_err := time.ParseInLocation( "02Jan2006 15:04:05.000" , old_tally.Modified , location ); modified_err == nil {
			stamp = hlc.FromTime( modified )
		}
		converted.Records = append( converted.Records , envelope( visits.TallyCollection , day.Format( visits.DayLayout ) , stamp , visits.Tally{ ShoppedFor: old_tally.ShoppedFor , Note: old_tally.Note } ) )
		report.Tallies++
	}

	if options.Settings {
		source := raw_config
		if options.SettingsJSON != "" {
			if source , err = os.ReadFile( options.SettingsJSON ); err != nil { return }
		}
		if len( source ) > 0 {
			records , names := convertSettings( source )
			converted.Records = append( converted.Records , records... )
			report.Settings = names
		}
	}
	sort.Slice( report.Problems , func( i , j int ) bool { return report.Problems[ i ].Key < report.Problems[ j ].Key } )
	return
}

func envelope( collection string , id string , stamp hlc.Stamp , body any ) ( result syncproto.Envelope ) {
	raw , _ := json.Marshal( body )
	if stamp.IsZero() {
		stamp = hlc.Stamp{ Wall: 1 }
	}
	result = syncproto.Envelope{ Collection: collection , ID: id , HLC: stamp , Node: Node , Body: raw }
	return
}

// visitID is the check-in's ULID, or a stable hash of what identifies it for
// the oldest records that have none -- the same rule the old system used to
// tell two of them apart.
func visitID( client_id string , check_in oldCheckIn , position int ) ( id string , has_ulid bool ) {
	if check_in.ULID != "" && syncproto.ValidID( check_in.ULID ) {
		id , has_ulid = check_in.ULID , true
		return
	}
	sum := encryption.Sha256Sum( [][]byte{ []byte( fmt.Sprintf( "%s|%s|%s|%s" , client_id , check_in.Date , check_in.Time , check_in.Type ) ) } )
	id = fmt.Sprintf( "legacy-%x" , sum[ :12 ] )
	return
}

// ---------------------------------------------------------------------------
// the settings
// ---------------------------------------------------------------------------

// settingsMap is old config key -> new setting path, for the business
// settings only. Nothing secret is ever carried over.
var settingsMap = map[string]string{
	"max_family_size":                "max_family_size",
	"balance.general.total":          "balance.general.total",
	"balance.general.tops":           "balance.general.tops",
	"balance.general.bottoms":        "balance.general.bottoms",
	"balance.general.dresses":        "balance.general.dresses",
	"balance.shoes":                  "balance.shoes",
	"balance.seasonals":              "balance.seasonals",
	"balance.accessories":            "balance.accessories",
	"check_in_cooloff_days":          "check_in_cooloff_days",
	"levenshtein_distance_threshold": "similarity_threshold",
	"auto_verify_zipcode":            "auto_verify_zipcode",
	"time_zone":                      "time_zone",
}

func flatten( prefix string , value any , out map[string]any ) {
	if nested , ok := value.( map[string]any ); ok {
		for name , child := range nested {
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			flatten( path , child , out )
		}
		return
	}
	out[ prefix ] = value
}

func convertSettings( raw []byte ) ( records []syncproto.Envelope , names []string ) {
	var tree map[string]any
	if json.Unmarshal( raw , &tree ) != nil { return }
	flat := map[string]any{}
	flatten( "" , tree , flat )
	// Old settings are stamped at the epoch's first millisecond: any value an
	// admin sets in the new system, ever, wins over them.
	stamp := hlc.Stamp{ Wall: 1 }
	for old_name , path := range settingsMap {
		value , ok := flat[ old_name ]
		if ok == false { continue }
		if text , is_text := value.( string ); is_text && text == "" { continue }
		if number , is_number := value.( float64 ); is_number && number == 0 && path != "max_family_size" { continue }
		normalized , valid := settings.Normalize( path , value )
		if valid == false || normalized == settings.Default( path ) { continue }
		raw_value , _ := json.Marshal( normalized )
		records = append( records , envelope( settings.Collection , path , stamp , map[string]json.RawMessage{ "value": raw_value } ) )
		names = append( names , path )
	}
	sort.Strings( names )
	return
}
