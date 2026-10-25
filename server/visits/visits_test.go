package visits

import (
	testing "testing"
	time "time"

	settings "github.com/0187773933/MastersClosetTracker/server/settings"
)

var newYork , _ = time.LoadLocation( "America/New_York" )

func at( text string ) ( result time.Time ) {
	result , _ = time.ParseInLocation( "2006-01-02 15:04" , text , newYork )
	return
}

func visitAt( id string , when string , kind string , items map[string]int ) ( entry Entry ) {
	moment := at( when )
	entry = Entry{ ID: id , Visit: Visit{ ClientID: "c1" , At: moment.UTC() , Day: DayKey( moment , newYork ) , Kind: kind , ShoppingFor: 2 , Items: items } }
	return
}

func TestDayKeyUsesTheClosetsTimeZone( t *testing.T ) {
	// 2am UTC on the 15th is still the evening of the 14th in New York.
	moment := time.Date( 2026 , 6 , 15 , 2 , 0 , 0 , 0 , time.UTC )
	if day := DayKey( moment , newYork ); day != "2026-06-14" {
		t.Fatalf( "day = %s" , day )
	}
}

func TestEvaluateCountsFromTheStartOfTheLastVisitsDay( t *testing.T ) {
	if outcome := Evaluate( nil , at( "2026-06-01 10:00" ) , 30 , newYork ); outcome.Allowed == false || outcome.FirstEver == false {
		t.Fatalf( "first visit: %+v" , outcome )
	}
	history := []Entry{ visitAt( "v1" , "2026-05-01 16:00" , KindNormal , nil ) }
	if outcome := Evaluate( history , at( "2026-05-31 09:00" ) , 30 , newYork ); outcome.Allowed == false {
		t.Fatalf( "30 days after the start of May 1st should be allowed: %+v" , outcome )
	}
	outcome := Evaluate( history , at( "2026-05-20 09:00" ) , 30 , newYork )
	if outcome.Allowed || outcome.DaysRemaining != 10 {
		t.Fatalf( "mid-cooloff: %+v" , outcome )
	}
	// A refused attempt is not a visit.
	history = append( history , visitAt( "v2" , "2026-05-30 09:00" , KindDenied , nil ) )
	if outcome := Evaluate( history , at( "2026-05-31 09:00" ) , 30 , newYork ); outcome.Allowed == false {
		t.Fatalf( "a denied attempt must not restart the cooloff: %+v" , outcome )
	}
}

func TestComputeBalanceOnlyCountsTheCurrentWindow( t *testing.T ) {
	allowance := settings.Balance{ General: settings.BalanceGeneral{ Total: 6 , Tops: 6 , Bottoms: 3 , Dresses: 1 } , Shoes: 1 , Seasonals: 1 , Accessories: 2 }
	history := []Entry{
		visitAt( "old" , "2026-03-01 10:00" , KindNormal , map[string]int{ ItemTops: 99 } ),
		visitAt( "recent" , "2026-05-25 10:00" , KindNormal , map[string]int{ ItemTops: 12 , ItemShoes: 2 , ItemAccessories: 1 } ),
		visitAt( "refused" , "2026-05-26 10:00" , KindDenied , map[string]int{ ItemTops: 50 } ),
	}
	balance := ComputeBalance( history , 3 , allowance , at( "2026-06-01 10:00" ) , 30 , newYork )
	if balance.General.Tops != ( Line{ Limit: 18 , Used: 12 , Available: 6 } ) {
		t.Fatalf( "tops: %+v" , balance.General.Tops )
	}
	if balance.Shoes != ( Line{ Limit: 3 , Used: 2 , Available: 1 } ) || balance.Accessories.Available != 5 {
		t.Fatalf( "shoes/accessories: %+v %+v" , balance.Shoes , balance.Accessories )
	}
	if balance.General.Total != 18 || balance.General.Available != 6 {
		t.Fatalf( "general: %+v" , balance.General )
	}
}

func TestShoppingVisitItemsAndClean( t *testing.T ) {
	request := ShoppingRequest{ ShoppingFor: 3 , ClothingPerPerson: 0 , PantsLimit: 3 , ShoesLimit: 1 , AccessoriesLimit: 2 , SeasonalLimit: 1 , Boys: -4 , Guests: 2 }.Clean( 6 )
	if request.ClothingPerPerson != 6 || request.Boys != 0 {
		t.Fatalf( "clean: %+v" , request )
	}
	visit := NewShoppingVisit( "c1" , "Lopez" , request , "9123456789" , at( "2026-06-01 10:00" ) , newYork , "front-desk" )
	if visit.Items[ ItemTops ] != 18 || visit.Items[ ItemBottoms ] != 9 || visit.Items[ ItemAccessories ] != 6 || visit.Guests != 2 {
		t.Fatalf( "items: %+v" , visit )
	}
	if visit.Day != "2026-06-01" || visit.Ticket.Barcode != "9123456789" || visit.Kind != KindNormal {
		t.Fatalf( "visit: %+v" , visit )
	}
}

func TestSummarizeIncludesPaperOnlyDaysNewestFirst( t *testing.T ) {
	entries := []Entry{
		visitAt( "a" , "2026-06-06 10:00" , KindNormal , nil ),
		visitAt( "b" , "2026-06-06 11:00" , KindForced , nil ),
		visitAt( "c" , "2026-06-06 12:00" , KindDenied , nil ),
		visitAt( "d" , "2026-06-13 10:00" , KindNormal , nil ),
	}
	days := Summarize( entries , map[string]Tally{ "2026-06-20": { ShoppedFor: 7 } , "2026-06-06": { ShoppedFor: 3 } } )
	if len( days ) != 3 || days[ 0 ].Day != "2026-06-20" || days[ 0 ].ManualShoppedFor != 7 || days[ 0 ].Count != 0 {
		t.Fatalf( "days: %+v" , days )
	}
	if days[ 2 ].Day != "2026-06-06" || days[ 2 ].Count != 2 || days[ 2 ].ShoppedFor != 4 || days[ 2 ].ManualShoppedFor != 3 {
		t.Fatalf( "june 6: %+v" , days[ 2 ] )
	}
}
