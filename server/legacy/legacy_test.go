package legacy

import (
	json "encoding/json"
	fmt "fmt"
	filepath "path/filepath"
	testing "testing"
	time "time"

	bolt "go.etcd.io/bbolt"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
	settings "github.com/0187773933/MastersClosetTracker/server/settings"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

const oldKey = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

// A v1-era record: check-ins carry a print_job, one has no ULID, no barcode.
const v1User = `{
	"uuid": "11111111-1111-4111-8111-111111111111", "username": "Ana-Lopez", "name_string": "Ana Lopez",
	"verified": true, "barcodes": null, "email_address": "", "phone_number": "555",
	"identity": {"first_name": "ana", "last_name": "lopez", "address": {"zipcode": "45424"}, "date_of_birth": {"month": "May", "day": 2, "year": 1980}},
	"family_size": 3, "family_members": [{"first_name": "Leo", "age": 7, "sex": "male"}, {"age": "34", "sex": "female", "spouse": true}],
	"spanish": true, "created_date": "05MAR2023", "created_time": "10:00:00.000",
	"balance": {"shoes": {"available": 1}}, "similar_users": null, "time_remaining": 0,
	"check_ins": [
		{"uuid": "11111111-1111-4111-8111-111111111111", "date": "05MAR2023", "time": "10:05:00.000", "type": "forced", "result": true,
		 "print_job": {"family_size": 3, "total_clothing_items": 18, "pants_limit": 3, "shoes_limit": 1, "shoes": 3, "seasonal": 3, "accessories": 6, "guests": 1, "spanish": true, "barcode_number": "123", "family_name": "Ana Lopez"}},
		{"uuid": "11111111-1111-4111-8111-111111111111", "ULID": "01GVA00000000000000000000A", "date": "10APR2023", "time": "11:00:00.000", "type": "first", "result": true}
	],
	"failed_check_ins": [{"date": "12APR2023", "time": "09:00:00.000", "type": "normal", "result": false, "time_remaining": 86400000}]
}`

// A v2-era record: shopping tickets, a modified time, a real barcode.
const v2User = `{
	"uuid": "22222222-2222-4222-8222-222222222222", "name_string": "Bo Kim", "verified": false,
	"barcodes": ["4455"], "identity": {"first_name": "Bo", "last_name": "Kim"}, "family_size": 1,
	"family_members": [], "language": "ko", "spanish": false,
	"created_date": "01JAN2026", "created_time": "08:00:00.000", "modified_date": "02JAN2026", "modified_time": "08:00:00.000",
	"check_ins": [
		{"ULID": "01JGB00000000000000000000B", "date": "02JAN2026", "time": "09:30:00.000", "type": "normal", "result": true, "name": "Bo Kim",
		 "shopping": {"shopping_for": 1, "tops": 6, "bottoms": 3, "shoes": 1, "guests": 2}}
	]
}`

func writeOldDB( t *testing.T , users map[string]string , config string , tallies map[string]string ) ( path string ) {
	t.Helper()
	path = filepath.Join( t.TempDir() , "old.db" )
	handle , err := bolt.Open( path , 0o600 , nil )
	if err != nil {
		t.Fatal( err )
	}
	defer handle.Close()
	handle.Update( func( tx *bolt.Tx ) error {
		bucket , _ := tx.CreateBucketIfNotExists( []byte( "users" ) )
		for id , record := range users {
			sealed , _ := encryption.LegacyChaChaSeal( oldKey , []byte( record ) )
			bucket.Put( []byte( id ) , sealed )
		}
		bucket.Put( []byte( "garbage" ) , []byte( "not encrypted with that key at all" ) )
		if config != "" {
			config_bucket , _ := tx.CreateBucketIfNotExists( []byte( "config" ) )
			config_bucket.Put( []byte( "current" ) , []byte( config ) )
		}
		manual , _ := tx.CreateBucketIfNotExists( []byte( "checkin-day-manual" ) )
		for day , record := range tallies {
			manual.Put( []byte( day ) , []byte( record ) )
		}
		return nil
	} )
	return
}

func station( t *testing.T ) *docs.Store {
	t.Helper()
	store := testutil.OpenStore( t , filepath.Join( t.TempDir() , "station.db" ) , docs.Buckets... )
	t.Cleanup( func() { store.Close() } )
	docs_store , err := docs.Open( store , nil )
	if err != nil {
		t.Fatal( err )
	}
	return docs_store
}

func TestImportV1AndV2Records( t *testing.T ) {
	path := writeOldDB( t ,
		map[string]string{ "11111111-1111-4111-8111-111111111111": v1User , "22222222-2222-4222-8222-222222222222": v2User },
		`{"check_in_cooloff_days": 21, "balance": {"shoes": 1, "general": {"total": 8}}, "admin_password": "never imported", "time_zone": "America/New_York"}`,
		map[string]string{ "06JUN2026": `{"shopped_for": 4, "note": "rain", "modified": "06JUN2026 18:00:00.000"}` },
	)
	store := station( t )
	converted , err := Read( Options{ DBPath: path , Key: oldKey , Settings: true , TimeZone: "America/New_York" } )
	if err != nil {
		t.Fatal( err )
	}
	report := converted.Report
	if report.Users != 3 || len( report.Problems ) != 1 || report.Clients != 2 || report.Visits != 3 || report.Refused != 1 || report.VisitsNoULID != 1 || report.Tallies != 1 || report.DerivedBarcode != 1 {
		t.Fatalf( "report: %+v" , report )
	}
	if fmt.Sprint( report.Settings ) != "[balance.general.total check_in_cooloff_days]" {
		t.Fatalf( "settings imported: %v (shoes=1 and the time zone are defaults; secrets never)" , report.Settings )
	}

	applied , err := Apply( store , converted , false )
	if err != nil || applied.Applied != len( converted.Records ) || len( applied.NewClientIDs ) != 2 {
		t.Fatalf( "apply: %+v %v" , applied , err )
	}
	if store.PendingCount() != len( converted.Records ) {
		t.Fatalf( "imported records must be queued for the hub" )
	}

	var ana clients.Client
	envelope , _ := store.Get( clients.Collection , "11111111-1111-4111-8111-111111111111" )
	docs.Decode( envelope , &ana )
	if ana.Name() != "Ana Lopez" || ana.Language != "es" || ana.Verified == false || len( ana.FamilyMembers ) != 2 || ana.FamilyMembers[ 1 ].Age != 34 || ana.FamilyMembers[ 0 ].FirstName != "Leo" {
		t.Fatalf( "ana: %+v" , ana )
	}
	if len( ana.Barcodes ) != 1 || ana.Barcodes[ 0 ][ 0 ] != '9' || ana.Extra != nil {
		t.Fatalf( "ana should get a derived barcode and no stray extras: %+v" , ana )
	}
	if ana.CreatedAt.In( time.UTC ).Format( time.RFC3339 ) != "2023-03-05T15:00:00Z" {
		t.Fatalf( "created_at in the old time zone: %v" , ana.CreatedAt )
	}

	var forced visits.Visit
	found := false
	store.ForEach( visits.Collection , func( envelope docs.Envelope ) bool {
		var visit visits.Visit
		docs.Decode( envelope , &visit )
		if visit.LegacyType == "forced" {
			forced , found = visit , true
		}
		return true
	} )
	if found == false || forced.Kind != visits.KindForced || forced.Day != "2023-03-05" || forced.ShoppingFor != 3 || forced.Items[ visits.ItemTops ] != 18 || forced.Items[ visits.ItemBottoms ] != 9 || forced.Guests != 1 || forced.Ticket == nil || forced.Ticket.ClothingPerPerson != 6 || forced.Ticket.Language != "es" {
		t.Fatalf( "forced visit: %+v ticket %+v" , forced , forced.Ticket )
	}

	var tally visits.Tally
	envelope , _ = store.Get( visits.TallyCollection , "2026-06-06" )
	docs.Decode( envelope , &tally )
	if tally.ShoppedFor != 4 || tally.Note != "rain" {
		t.Fatalf( "tally: %+v" , tally )
	}
	envelope , _ = store.Get( settings.Collection , "check_in_cooloff_days" )
	var record struct{ Value json.RawMessage `json:"value"` }
	docs.Decode( envelope , &record )
	if string( record.Value ) != "21" || envelope.HLC.Wall != 1 {
		t.Fatalf( "imported settings should hold the old value at the lowest stamp: %s %v" , record.Value , envelope.HLC )
	}

	// Importing the same file again changes nothing.
	again , _ := Read( Options{ DBPath: path , Key: oldKey , Settings: true , TimeZone: "America/New_York" } )
	second , err := Apply( store , again , false )
	if err != nil || second.Applied != 0 || second.AlreadyThere != len( again.Records ) || len( second.NewClientIDs ) != 0 {
		t.Fatalf( "re-import should be a no-op: %+v %v" , second , err )
	}
}

func TestOlderDatabaseMergesAndADryRunListsWhatIsNew( t *testing.T ) {
	newer := writeOldDB( t , map[string]string{ "22222222-2222-4222-8222-222222222222": v2User } , "" , nil )
	// The same family, edited earlier, plus a family the newer database no
	// longer has (deleted there).
	older_bo := `{"uuid": "22222222-2222-4222-8222-222222222222", "identity": {"first_name": "Bo", "last_name": "Kim-Old"},
		"created_date": "01JAN2026", "created_time": "08:00:00.000", "modified_date": "01JAN2026", "modified_time": "08:00:00.000"}`
	older := writeOldDB( t , map[string]string{ "22222222-2222-4222-8222-222222222222": older_bo , "11111111-1111-4111-8111-111111111111": v1User } , "" , nil )

	store := station( t )
	converted , _ := Read( Options{ DBPath: newer , Key: oldKey } )
	Apply( store , converted , false )

	converted , _ = Read( Options{ DBPath: older , Key: oldKey } )
	dry , err := Apply( store , converted , true )
	if err != nil || len( dry.NewClientIDs ) != 1 || dry.NewClientIDs[ 0 ] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf( "dry run should name exactly the family only the older database has: %+v %v" , dry.NewClientIDs , err )
	}
	if _ , err := store.Get( clients.Collection , "11111111-1111-4111-8111-111111111111" ); err == nil {
		t.Fatalf( "a dry run must write nothing" )
	}
	Apply( store , converted , false )
	var bo clients.Client
	envelope , _ := store.Get( clients.Collection , "22222222-2222-4222-8222-222222222222" )
	docs.Decode( envelope , &bo )
	if bo.Identity.LastName != "Kim" {
		t.Fatalf( "the more recently edited profile must win: %q" , bo.Identity.LastName )
	}
}
