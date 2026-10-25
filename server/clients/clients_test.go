package clients

import (
	json "encoding/json"
	strings "strings"
	testing "testing"
	time "time"
)

func members( n int ) ( out []Person ) {
	out = make( []Person , n )
	for index := range out {
		out[ index ] = Person{ Age: 10 + index }
	}
	return
}

func TestExtrasRoundTripAndDerivedKeysAreDropped( t *testing.T ) {
	// What an edit page sends back: the view it was shown, edited.
	sent := `{
		"uuid": "abc", "_rev": "x", "name_string": "Old Name", "check_ins": [{"date":"01JAN2026"}],
		"balance": {"shoes": {"available": 1}}, "similar_users": [], "spanish": true,
		"identity": {"first_name": "maría", "last_name": "lópez"},
		"favorite_color": "green", "notes": {"allergies": "none"}
	}`
	var client Client
	if err := json.Unmarshal( []byte( sent ) , &client ); err != nil {
		t.Fatal( err )
	}
	if client.Language != "es" {
		t.Fatalf( "legacy spanish flag should become language es, got %q" , client.Language )
	}
	if len( client.Extra ) != 2 || string( client.Extra[ "favorite_color" ] ) != `"green"` {
		t.Fatalf( "extras = %v" , client.Extra )
	}
	saved , _ := json.Marshal( client )
	for _ , derived_key := range []string{ "check_ins" , "balance" , "uuid" , "_rev" , "name_string" , "similar_users" , "spanish" } {
		if strings.Contains( string( saved ) , `"`+derived_key+`"` ) {
			t.Fatalf( "derived key %q was stored: %s" , derived_key , saved )
		}
	}
	if strings.Contains( string( saved ) , `"allergies"` ) == false {
		t.Fatalf( "unknown nested extra lost: %s" , saved )
	}
}

func TestNormalizeAndNames( t *testing.T ) {
	client := Client{
		Identity: Person{ FirstName: "  maría  elena", MiddleName: "" , LastName: "LÓPEZ" , Address: Address{ ZipCode: " 45424 " } },
		Barcodes: []string{ " 123 " , "" , "123" , "456" },
		FamilyMembers: members( 3 ),
		FamilySize:    1,
	}
	Normalize( &client )
	if client.Name() != "María Elena López" || client.Username() != "María-Elena-López" {
		t.Fatalf( "names: %q %q" , client.Name() , client.Username() )
	}
	if strings.Join( client.Barcodes , "," ) != "123,456" {
		t.Fatalf( "barcodes: %v" , client.Barcodes )
	}
	if client.FamilySize != 4 {
		t.Fatalf( "family size should cover the listed members, got %d" , client.FamilySize )
	}
	AutoVerify( &client , "45424" )
	if client.Verified == false {
		t.Fatalf( "local zipcode should verify" )
	}
	tokens := strings.Join( client.SearchTokens() , " " )
	if tokens != "maria elena lopez" {
		t.Fatalf( "search tokens should be folded: %q" , tokens )
	}
}

func TestClampFamilyCapsNewRecord( t *testing.T ) {
	client := Client{ FamilySize: 9 , FamilyMembers: members( 8 ) }
	ClampFamily( &client , 6 , 0 )
	if client.FamilySize != 6 || len( client.FamilyMembers ) != 5 {
		t.Fatalf( "size=%d members=%d, want 6 and 5 (the cap counts the holder)" , client.FamilySize , len( client.FamilyMembers ) )
	}
}

func TestClampFamilyNeverShrinksExisting( t *testing.T ) {
	client := Client{ FamilySize: 9 , FamilyMembers: members( 8 ) }
	ClampFamily( &client , 6 , 9 )
	if client.FamilySize != 9 || len( client.FamilyMembers ) != 8 {
		t.Fatalf( "an already-oversized family must keep its members" )
	}
	client = Client{ FamilySize: 12 , FamilyMembers: members( 11 ) }
	ClampFamily( &client , 6 , 9 )
	if client.FamilySize != 9 || len( client.FamilyMembers ) != 8 {
		t.Fatalf( "growth past the existing size must be refused" )
	}
	client = Client{ FamilySize: 30 , FamilyMembers: members( 29 ) }
	ClampFamily( &client , 0 , 0 )
	if client.FamilySize != 30 {
		t.Fatalf( "0 must mean no limit" )
	}
}

// A record whose cached size fell behind its members (legacy data, or a size
// the editor never recomputed) is what made the check-in screen show a family
// of one. A larger stored size with nothing itemized predates the
// family-members editor and must survive. And the floor, applied before the
// cap, can never push a record past it.
func TestFamilySizeFloorsAtMembers( t *testing.T ) {
	client := Client{ FamilySize: 1 , FamilyMembers: members( 4 ) }
	Normalize( &client )
	if client.FamilySize != 5 {
		t.Fatalf( "family_size = %d, want 5 (self + 4 members)" , client.FamilySize )
	}
	client = Client{ FamilySize: 6 }
	Normalize( &client )
	if client.FamilySize != 6 {
		t.Fatalf( "family_size = %d, want 6 (no members itemized)" , client.FamilySize )
	}
	client = Client{ FamilySize: 1 , FamilyMembers: members( 8 ) }
	Normalize( &client )
	ClampFamily( &client , 6 , 0 )
	if client.FamilySize != 6 || len( client.FamilyMembers ) != 5 {
		t.Fatalf( "size=%d members=%d, want 6 and 5" , client.FamilySize , len( client.FamilyMembers ) )
	}
}

func TestBarcodes( t *testing.T ) {
	calls := 0
	barcode , err := MintBarcode( func( string ) bool { calls++; return calls < 3 } )
	if err != nil || len( barcode ) != 10 || barcode[ 0 ] != '9' || calls != 3 {
		t.Fatalf( "mint: %q %v after %d tries" , barcode , err , calls )
	}
	none := func( string ) bool { return false }
	if DerivedBarcode( "client-1" , none ) != DerivedBarcode( "client-1" , none ) {
		t.Fatalf( "derived barcode must be stable" )
	}
	first := DerivedBarcode( "client-1" , none )
	if DerivedBarcode( "client-1" , func( b string ) bool { return b == first } ) == first {
		t.Fatalf( "derived barcode must re-roll on collision" )
	}
}

func TestStampBarcodes( t *testing.T ) {
	before := time.Date( 2026 , 1 , 1 , 0 , 0 , 0 , 0 , time.UTC )
	now := time.Date( 2026 , 6 , 1 , 0 , 0 , 0 , 0 , time.UTC )
	previous := &Client{ Barcodes: []string{ "dated" , "undated" , "removed" } , BarcodeAssigned: map[string]time.Time{ "dated": before , "removed": before } }
	// The page claims dates of its own; they must not be trusted.
	client := &Client{ Barcodes: []string{ "dated" , "undated" , "new" } , BarcodeAssigned: map[string]time.Time{ "dated": now , "undated": now } }
	StampBarcodes( client , previous , now )
	got := client.BarcodeAssigned
	if len( got ) != 2 || got[ "dated" ] != before || got[ "new" ] != now {
		t.Fatalf( "kept dates, stamped new, dropped removed, left the old undated one alone: %v" , got )
	}
	if _ , ok := got[ "undated" ]; ok {
		t.Fatalf( "a barcode from before dates were recorded must not be dated today" )
	}
	fresh := &Client{ Barcodes: []string{ "a" } }
	StampBarcodes( fresh , nil , now )
	if fresh.BarcodeAssigned[ "a" ] != now {
		t.Fatalf( "a new record's barcodes are assigned now: %v" , fresh.BarcodeAssigned )
	}
}

func TestCompare( t *testing.T ) {
	a := &Client{ Identity: Person{ FirstName: "Jose" , LastName: "Perez" } , PhoneNumber: "555" }
	b := &Client{ Identity: Person{ FirstName: "José" , LastName: "Peres" } , PhoneNumber: "555" , Barcodes: []string{ "1" } }
	similar := Compare( a , b , 3 )
	if similar.Name == false || similar.Phone == false || similar.Barcode || similar.Any() == false {
		t.Fatalf( "compare: %+v" , similar )
	}
	if Levenshtein( "kitten" , "sitting" ) != 3 || Levenshtein( "" , "abc" ) != 3 {
		t.Fatalf( "levenshtein broken" )
	}
}
