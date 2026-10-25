package index

import (
	filepath "path/filepath"
	testing "testing"
	time "time"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

func setup( t *testing.T ) ( *Index , *docs.Store ) {
	t.Helper()
	store := testutil.OpenStore( t , filepath.Join( t.TempDir() , "a.db" ) , docs.Buckets... )
	t.Cleanup( func() { store.Close() } )
	docs_store , err := docs.Open( store , nil )
	if err != nil {
		t.Fatal( err )
	}
	index := New()
	if err := index.Attach( docs_store ); err != nil {
		t.Fatal( err )
	}
	return index , docs_store
}

func person( first string , last string ) ( result clients.Person ) {
	result = clients.Person{ FirstName: first , LastName: last }
	return
}

func TestSearchPrefixTypoAndAccents( t *testing.T ) {
	index , store := setup( t )
	store.Put( clients.Collection , "c1" , clients.Client{ Identity: person( "José" , "Pearson" ) } )
	store.Put( clients.Collection , "c2" , clients.Client{ Identity: person( "Joseph" , "Smith" ) } )
	store.Put( clients.Collection , "c3" , clients.Client{ Identity: person( "Ana" , "Lopez" ) , AuthorizedAliases: []clients.Person{ person( "Pedro" , "Lopez" ) } } )

	check := func( query string , want ...string ) {
		t.Helper()
		var got []string
		for _ , hit := range index.Search( query , 25 ) {
			got = append( got , hit.ID )
		}
		if len( got ) != len( want ) {
			t.Fatalf( "search %q = %v, want %v" , query , got , want )
		}
		for position := range want {
			if got[ position ] != want[ position ] {
				t.Fatalf( "search %q = %v, want %v" , query , got , want )
			}
		}
	}
	check( "jose" , "c1" , "c2" )      // exact (accent folded) before prefix
	check( "pear" , "c1" )             // prefix while typing
	check( "pearsen" , "c1" )          // one typo
	check( "jose pearson" , "c1" )     // every word must match
	check( "pedro" , "c3" )            // authorised shopper's name
	check( "zz" )
}

func TestDeletedClientHidesItsVisitsAndBarcodeStaysReserved( t *testing.T ) {
	index , store := setup( t )
	store.Put( clients.Collection , "c1" , clients.Client{ Identity: person( "Ana" , "Lopez" ) , Barcodes: []string{ "9000000001" } } )
	moment := time.Date( 2026 , 6 , 6 , 15 , 0 , 0 , 0 , time.UTC )
	store.Put( visits.Collection , "v1" , visits.Visit{ ClientID: "c1" , At: moment , Day: "2026-06-06" , Kind: visits.KindNormal , ShoppingFor: 2 } )
	store.Put( visits.Collection , "v-orphan" , visits.Visit{ ClientID: "not-synced-yet" , At: moment , Day: "2026-06-06" , Kind: visits.KindNormal } )

	if days := index.Days(); len( days ) != 1 || days[ 0 ].Count != 1 {
		t.Fatalf( "an orphan visit must not count: %+v" , days )
	}
	if len( index.ByBarcode( "9000000001" ) ) != 1 {
		t.Fatalf( "barcode lookup failed" )
	}
	store.Delete( clients.Collection , "c1" )
	if len( index.Day( "2026-06-06" ) ) != 0 || len( index.Days() ) != 0 || len( index.Search( "ana" , 10 ) ) != 0 {
		t.Fatalf( "a deleted client's visits must disappear" )
	}
	if len( index.ByBarcode( "9000000001" ) ) != 0 || index.BarcodeTaken( "9000000001" ) == false {
		t.Fatalf( "a deleted client's barcode must not resolve but must stay reserved" )
	}
}

func TestSharedBarcodeReturnsBothAndRecentOrder( t *testing.T ) {
	index , store := setup( t )
	store.Put( clients.Collection , "a" , clients.Client{ Identity: person( "A" , "A" ) , Barcodes: []string{ "777" } } )
	store.Put( clients.Collection , "b" , clients.Client{ Identity: person( "B" , "B" ) , Barcodes: []string{ "777" } } )
	if len( index.ByBarcode( "777" ) ) != 2 {
		t.Fatalf( "both families sharing a card number must be returned" )
	}
	base := time.Date( 2026 , 6 , 6 , 15 , 0 , 0 , 0 , time.UTC )
	store.Put( visits.Collection , "va" , visits.Visit{ ClientID: "a" , At: base , Day: "2026-06-06" , Kind: visits.KindNormal } )
	store.Put( visits.Collection , "vb" , visits.Visit{ ClientID: "b" , At: base.Add( time.Hour ) , Day: "2026-06-06" , Kind: visits.KindNormal } )
	store.Put( visits.Collection , "va2" , visits.Visit{ ClientID: "a" , At: base.Add( 2 * time.Hour ) , Day: "2026-06-06" , Kind: visits.KindDenied } )
	recent := index.Recent( 10 )
	if len( recent ) != 2 || recent[ 0 ].Client.ID != "b" || recent[ 1 ].Count != 1 {
		t.Fatalf( "recent: %+v" , recent )
	}
}

func TestFirstVisitsSkipsDenied( t *testing.T ) {
	index , store := setup( t )
	store.Put( clients.Collection , "a" , clients.Client{ Identity: person( "Ann" , "A" ) } )
	store.Put( clients.Collection , "b" , clients.Client{ Identity: person( "Bob" , "B" ) } )
	base := time.Date( 2026 , 6 , 6 , 10 , 0 , 0 , 0 , time.UTC )
	store.Put( visits.Collection , "a-denied" , visits.Visit{ ClientID: "a" , At: base , Day: "2026-06-06" , Kind: visits.KindDenied } )
	store.Put( visits.Collection , "a-first" , visits.Visit{ ClientID: "a" , At: base.Add( time.Hour ) , Day: "2026-06-06" , Kind: visits.KindNormal } )
	store.Put( visits.Collection , "a-later" , visits.Visit{ ClientID: "a" , At: base.Add( 48 * time.Hour ) , Day: "2026-06-08" , Kind: visits.KindNormal } )
	firsts := index.FirstVisits( []string{ "a" , "b" } )
	if firsts[ "a" ] != "a-first" {
		t.Fatalf( "first visit of a = %q, want a-first" , firsts[ "a" ] )
	}
	if _ , ok := firsts[ "b" ]; ok {
		t.Fatalf( "b has no visits but got %q" , firsts[ "b" ] )
	}
}
