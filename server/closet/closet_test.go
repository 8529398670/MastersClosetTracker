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
