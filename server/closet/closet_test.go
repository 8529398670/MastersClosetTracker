package closet_test

import (
	json "encoding/json"
	errors "errors"
	filepath "path/filepath"
	testing "testing"
	time "time"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	closet "github.com/0187773933/MastersClosetTracker/server/closet"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	index "github.com/0187773933/MastersClosetTracker/server/index"
	settings "github.com/0187773933/MastersClosetTracker/server/settings"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
	views "github.com/0187773933/MastersClosetTracker/server/views"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

type clock struct{ at time.Time }

func ( fake *clock ) now() time.Time { return fake.at }

func stack( t *testing.T ) ( *closet.Service , *clock ) {
	t.Helper()
	fake := &clock{ at: time.Date( 2026 , 6 , 6 , 15 , 0 , 0 , 0 , time.UTC ) }
	store := testutil.OpenStore( t , filepath.Join( t.TempDir() , "a.db" ) , append( append( []string{} , docs.Buckets... ) , settings.Buckets... )... )
	t.Cleanup( func() { store.Close() } )
	docs_store , err := docs.Open( store , fake.now )
	if err != nil {
		t.Fatal( err )
	}
	settings_service , err := settings.New( docs_store , store , testutil.Config( t ) )
	if err != nil {
		t.Fatal( err )
	}
	idx := index.New()
	if err := idx.Attach( docs_store ); err != nil {
		t.Fatal( err )
	}
	return closet.New( docs_store , idx , settings_service , "front-desk" ) , fake
}

func TestCreateClientAppliesEveryRule( t *testing.T ) {
	service , _ := stack( t )
	created , err := service.CreateClient( clients.Client{
		Identity:      clients.Person{ FirstName: "ana" , LastName: "LOPEZ" , Address: clients.Address{ ZipCode: "45424" } },
		FamilyMembers: []clients.Person{ { Age: 4 } , { Age: 30 , Spouse: true } },
	} )
	if err != nil {
		t.Fatal( err )
	}
	record := created.Record
	if record.Name() != "Ana Lopez" || record.Verified == false || record.FamilySize != 3 || len( record.Barcodes ) != 1 || record.Barcodes[ 0 ][ 0 ] != '9' {
		t.Fatalf( "created: %+v" , record )
	}
	temp , _ := service.CreateClient( clients.Client{} )
	if temp.Record.Identity.FirstName != "Temp" || len( temp.Record.Identity.MiddleName ) != 6 {
		t.Fatalf( "a nameless client should become a numbered Temp record: %+v" , temp.Record.Identity )
	}
}

func TestEditFromAStalePageIsRefused( t *testing.T ) {
	service , fake := stack( t )
	created , _ := service.CreateClient( clients.Client{ Identity: clients.Person{ FirstName: "Ana" , LastName: "Lopez" } } )

	// What the edit page loaded, round-tripped through the view as the
	// browser would send it back.
	page := func( client index.Client ) ( input clients.Client , rev hlc.Stamp ) {
		standing , _ := service.Standing( client.ID )
		raw , _ := json.Marshal( views.User( client , &standing , time.UTC ) )
		json.Unmarshal( raw , &input )
		var echoed struct{ Rev string `json:"_rev"` }
		json.Unmarshal( raw , &echoed )
		rev , _ = hlc.Parse( echoed.Rev )
		return
	}
	tab_one , rev_one := page( created )
	tab_two , rev_two := page( created )

	fake.at = fake.at.Add( time.Minute )
	tab_one.PhoneNumber = "555-0100"
	updated , err := service.UpdateClient( created.ID , tab_one , rev_one )
	if err != nil {
		t.Fatal( err )
	}
	if updated.Record.CreatedAt.Equal( created.Record.CreatedAt ) == false || updated.Record.PhoneNumber != "555-0100" {
		t.Fatalf( "update lost created_at or the edit: %+v" , updated.Record )
	}
	tab_two.EmailAddress = "late@example.com"
	if _ , err := service.UpdateClient( created.ID , tab_two , rev_two ); errors.Is( err , closet.ErrStale ) == false {
		t.Fatalf( "the second tab should be told it is stale, got %v" , err )
	}
}

func TestRecordShoppingGuardAndStanding( t *testing.T ) {
	service , fake := stack( t )
	created , _ := service.CreateClient( clients.Client{ Identity: clients.Person{ FirstName: "Ana" , LastName: "Lopez" } , FamilyMembers: []clients.Person{ { Age: 8 } } } )

	request := visits.ShoppingRequest{ ShoppingFor: 2 , PantsLimit: 3 , ShoesLimit: 1 , AccessoriesLimit: 2 , SeasonalLimit: 1 , Guests: 1 }
	entry , _ , err := service.RecordShopping( created.ID , request )
	if err != nil {
		t.Fatal( err )
	}
	if entry.Visit.Items[ visits.ItemTops ] != 12 || entry.Visit.Ticket.Barcode != created.Record.Barcodes[ 0 ] || entry.Visit.Station != "front-desk" {
		t.Fatalf( "visit: %+v" , entry.Visit )
	}
	if _ , _ , err := service.RecordShopping( created.ID , request ); errors.Is( err , closet.ErrRecentVisit ) == false {
		t.Fatalf( "an immediate second check-in should be refused, got %v" , err )
	}
	again := request
	again.Again = true
	second , _ , err := service.RecordShopping( created.ID , again )
	if err != nil {
		t.Fatalf( "a confirmed second check-in should go through, got %v" , err )
	}
	// Leave the history as the rest of the test expects it.
	if err := service.DeleteVisit( second.ID ); err != nil {
		t.Fatal( err )
	}

	fake.at = fake.at.Add( 24 * time.Hour )
	standing , _ := service.Standing( created.ID )
	if standing.Outcome.Allowed || standing.Balance.General.Tops.Used != 12 || standing.Balance.General.Tops.Limit != 12 {
		t.Fatalf( "standing: %+v" , standing )
	}
	client , _ := service.Index.Client( created.ID )
	if client.HLC != created.HLC {
		t.Fatalf( "recording a visit must never write the client record" )
	}

	if err := service.DeleteVisit( entry.ID ); err != nil {
		t.Fatal( err )
	}
	if standing , _ := service.Standing( created.ID ); standing.Outcome.Allowed == false {
		t.Fatalf( "with the only visit deleted the family may come again" )
	}
}

func TestBarcodeDatesAndRows( t *testing.T ) {
	service , fake := stack( t )
	created_at := fake.at
	ana , err := service.CreateClient( clients.Client{ Identity: clients.Person{ FirstName: "Ana" , LastName: "Lopez" } , Barcodes: []string{ "1111" } } )
	if err != nil {
		t.Fatal( err )
	}
	if ana.Record.BarcodeAssigned[ "1111" ] != created_at {
		t.Fatalf( "a new family's barcode is dated: %v" , ana.Record.BarcodeAssigned )
	}

	fake.at = fake.at.Add( 48 * time.Hour )
	edit := ana.Record
	edit.Barcodes = []string{ "1111" , "2222" }
	ana , err = service.UpdateClient( ana.ID , edit , ana.HLC )
	if err != nil {
		t.Fatal( err )
	}
	if ana.Record.BarcodeAssigned[ "1111" ] != created_at || ana.Record.BarcodeAssigned[ "2222" ] != fake.at {
		t.Fatalf( "an edit keeps the first date and dates the added barcode: %v" , ana.Record.BarcodeAssigned )
	}

	// A family from before dates were recorded, written as an import would.
	fake.at = fake.at.Add( 24 * time.Hour )
	bo_registered := fake.at.Add( -30 * 24 * time.Hour )
	if _ , err := service.Docs.Put( clients.Collection , "bo" , clients.Client{ Identity: clients.Person{ FirstName: "Bo" } , Barcodes: []string{ "2222" , "3333" } , CreatedAt: bo_registered } ); err != nil {
		t.Fatal( err )
	}
	visit , _ , err := service.RecordShopping( "bo" , visits.ShoppingRequest{ ShoppingFor: 1 } )
	if err != nil {
		t.Fatal( err )
	}

	rows := service.BarcodeRows()
	if len( rows ) != 4 {
		t.Fatalf( "one row per barcode per family: %+v" , rows )
	}
	found := map[string]closet.BarcodeRow{}
	for _ , row := range rows {
		found[ row.ClientID+"/"+row.Barcode ] = row
	}
	if found[ ana.ID+"/2222" ].Owners != 2 || found[ "bo/2222" ].Owners != 2 || found[ ana.ID+"/1111" ].Owners != 1 {
		t.Fatalf( "2222 is shared by two families: %+v" , rows )
	}
	if row := found[ "bo/2222" ]; row.Estimated == false || row.AssignedAt.Equal( bo_registered ) == false {
		t.Fatalf( "an undated barcode falls back to registration: %+v" , row )
	}
	if row := found[ "bo/3333" ]; row.Estimated == false || row.AssignedAt.Equal( bo_registered ) == false {
		t.Fatalf( "a visit after registration does not make the estimate later: %+v (visit %v)" , row , visit.Visit.At )
	}
	if found[ ana.ID+"/1111" ].Estimated || rows[ 0 ] != found[ ana.ID+"/2222" ] || rows[ 1 ] != found[ ana.ID+"/1111" ] {
		t.Fatalf( "recorded dates are exact, and the newest sorts first: %+v" , rows )
	}
}

func TestBarcodeEstimateUsesFirstPrintedVisit( t *testing.T ) {
	service , fake := stack( t )
	// A legacy import with no registration date at all.
	if _ , err := service.Docs.Put( clients.Collection , "cy" , clients.Client{ Identity: clients.Person{ FirstName: "Cy" } , Barcodes: []string{ "4444" } } ); err != nil {
		t.Fatal( err )
	}
	if rows := service.BarcodeRows(); len( rows ) != 1 || rows[ 0 ].AssignedAt.IsZero() == false {
		t.Fatalf( "nothing tells when, so no date: %+v" , rows )
	}
	visit , _ , err := service.RecordShopping( "cy" , visits.ShoppingRequest{ ShoppingFor: 1 } )
	if err != nil {
		t.Fatal( err )
	}
	fake.at = fake.at.Add( time.Hour )
	if rows := service.BarcodeRows(); rows[ 0 ].Estimated == false || rows[ 0 ].AssignedAt.Equal( visit.Visit.At ) == false {
		t.Fatalf( "the first visit that printed the barcode dates it: %+v" , rows )
	}
}

func TestTallyKeepsTheNoteWhenOnlyTheCountIsSent( t *testing.T ) {
	service , _ := stack( t )
	note , count := "rain day" , 4
	service.SetTally( "2026-06-06" , &count , &note )
	count = 9
	tally , err := service.SetTally( "2026-06-06" , &count , nil )
	if err != nil || tally.ShoppedFor != 9 || tally.Note != "rain day" {
		t.Fatalf( "tally: %+v %v" , tally , err )
	}
	if _ , err := service.SetTally( "June 6th" , &count , nil ); errors.Is( err , closet.ErrInvalid ) == false {
		t.Fatalf( "a bad day should be refused" )
	}
}

func TestViewsDayLabels( t *testing.T ) {
	if views.DayLabel( "2026-06-27" ) != "27JUN2026" {
		t.Fatalf( "label: %s" , views.DayLabel( "2026-06-27" ) )
	}
	for _ , text := range []string{ "27JUN2026" , "27Jun2026" , "2026-06-27" } {
		if day , ok := views.ParseDay( text ); ok == false || day != "2026-06-27" {
			t.Fatalf( "parse %q -> %q %v" , text , day , ok )
		}
	}
	if _ , ok := views.ParseDay( "../../etc" ); ok {
		t.Fatalf( "junk must not parse" )
	}
}

func TestDuplicatesGroupThePersonNotTheHousehold( t *testing.T ) {
	service , _ := stack( t )
	home := clients.Address{ StreetNumber: "6627" , StreetName: "Grovehill Drive" }
	person := func( first string , last string , barcode string ) index.Client {
		created , err := service.CreateClient( clients.Client{
			Identity:    clients.Person{ FirstName: first , LastName: last , Address: home },
			PhoneNumber: "9379562035",
			Barcodes:    []string{ barcode },
		} )
		if err != nil {
			t.Fatal( err )
		}
		return created
	}
	// A mother and daughter at one address with one phone, each registered
	// three times: two people, not one group of six.
	for range 3 {
		person( "Chayenne" , "Stonebrook" , "00355" )
		person( "Chelsea" , "Whited" , "00354" )
	}
	// A shared barcode joins different spellings; a near-miss name alone
	// does not.
	typo , _ := service.CreateClient( clients.Client{ Identity: clients.Person{ FirstName: "Chelsie" , LastName: "Whited" } , Barcodes: []string{ "00354" } } )
	service.CreateClient( clients.Client{ Identity: clients.Person{ FirstName: "Ana" , LastName: "Lopez" } } )
	service.CreateClient( clients.Client{ Identity: clients.Person{ FirstName: "Anna" , LastName: "Lopes" } } )

	// Different people on one card number (hand-typed or reissued cards)
	// are a barcode problem, not a duplicate.
	for _ , name := range [][2]string{ { "Jannese" , "Jackson" } , { "Marie" , "Jackson" } , { "Amy" , "Wirick" } } {
		service.CreateClient( clients.Client{ Identity: clients.Person{ FirstName: name[ 0 ] , LastName: name[ 1 ] } , Barcodes: []string{ "00219" } } )
	}

	groups := service.Duplicates()
	if len( groups ) != 2 || len( groups[ 0 ].Members ) != 4 || len( groups[ 1 ].Members ) != 3 {
		t.Fatalf( "want a group of 4 (Chelsea x3 + barcode twin) and 3 (Chayenne), got %+v" , groups )
	}
	for _ , member := range groups[ 0 ].Members {
		if member.Client.Record.Name() == "Chayenne Stonebrook" {
			t.Fatalf( "a shared phone and address must not join two people" )
		}
		if member.Client.ID == typo.ID && ( len( member.Reasons ) != 1 || member.Reasons[ 0 ] != "barcode" ) {
			t.Fatalf( "the typo matched by barcode only: %v" , member.Reasons )
		}
	}
	if reasons := groups[ 1 ].Members[ 0 ].Reasons; len( reasons ) != 4 {
		t.Fatalf( "name, barcode, phone and address are all shared: %v" , reasons )
	}
}

func TestMergeClientsMovesEverythingToTheOneKept( t *testing.T ) {
	service , fake := stack( t )
	keep , _ := service.CreateClient( clients.Client{
		Identity:      clients.Person{ FirstName: "Ana" , LastName: "Lopez" },
		FamilyMembers: []clients.Person{ { Age: 8 } },
	} )
	fake.at = fake.at.Add( -48 * time.Hour )
	other , _ := service.CreateClient( clients.Client{
		Identity:      clients.Person{ FirstName: "Ana" , LastName: "Lopez" },
		PhoneNumber:   "555-0101",
		Verified:      true,
		FamilyMembers: []clients.Person{ { Age: 1 } , { Age: 2 } , { Age: 3 } },
	} )
	if _ , _ , err := service.RecordShopping( other.ID , visits.ShoppingRequest{ ShoppingFor: 1 } ); err != nil {
		t.Fatal( err )
	}
	fake.at = fake.at.Add( 48 * time.Hour )
	if _ , _ , err := service.RecordShopping( keep.ID , visits.ShoppingRequest{ ShoppingFor: 1 } ); err != nil {
		t.Fatal( err )
	}

	if _ , err := service.MergeClients( keep.ID , []string{ other.ID } , hlc.Stamp{ Wall: 1 } ); errors.Is( err , closet.ErrStale ) == false {
		t.Fatalf( "a merge from a stale page should be refused, got %v" , err )
	}
	if _ , err := service.MergeClients( keep.ID , []string{ keep.ID } , hlc.Stamp{} ); errors.Is( err , closet.ErrInvalid ) == false {
		t.Fatalf( "merging a family into itself should be refused, got %v" , err )
	}

	merged , err := service.MergeClients( keep.ID , []string{ other.ID } , keep.HLC )
	if err != nil {
		t.Fatal( err )
	}
	record := merged.Record
	if record.PhoneNumber != "555-0101" || record.Verified == false || len( record.FamilyMembers ) != 1 || len( record.Barcodes ) != 2 {
		t.Fatalf( "merged: %+v" , record )
	}
	if record.Barcodes[ 0 ] != keep.Record.Barcodes[ 0 ] || record.CreatedAt.Equal( other.Record.CreatedAt ) == false {
		t.Fatalf( "the kept barcode stays first and the earliest registration wins: %+v" , record )
	}
	moved := other.Record.Barcodes[ 0 ]
	if record.BarcodeAssigned[ moved ].Equal( other.Record.BarcodeAssigned[ moved ] ) == false {
		t.Fatalf( "a moved barcode keeps its date: %v vs %v" , record.BarcodeAssigned , other.Record.BarcodeAssigned )
	}
	if len( service.Index.VisitsOf( keep.ID ) ) != 2 || len( service.Index.VisitsOf( other.ID ) ) != 0 {
		t.Fatalf( "visits should all be on the kept family" )
	}
	if owners := service.Index.ByBarcode( moved ); len( owners ) != 1 || owners[ 0 ].ID != keep.ID {
		t.Fatalf( "the moved barcode should find the kept family: %+v" , owners )
	}
	if _ , ok := service.Index.Client( other.ID ); ok {
		t.Fatalf( "the merged-in family should be gone" )
	}
	if len( service.Duplicates() ) != 0 {
		t.Fatalf( "nothing left to merge" )
	}
}
