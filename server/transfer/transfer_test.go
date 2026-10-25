package transfer_test

import (
	fmt "fmt"
	filepath "path/filepath"
	testing "testing"
	time "time"

	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	hub "github.com/0187773933/MastersClosetTracker/server/hub"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
	transfer "github.com/0187773933/MastersClosetTracker/server/transfer"
)

func station( t *testing.T , name string ) *docs.Store {
	t.Helper()
	store := testutil.OpenStore( t , filepath.Join( t.TempDir() , name+".db" ) , docs.Buckets... )
	t.Cleanup( func() { store.Close() } )
	docs_store , err := docs.Open( store , nil )
	if err != nil {
		t.Fatal( err )
	}
	return docs_store
}

func snapshot( store *docs.Store ) ( result string ) {
	all := map[string]string{}
	for _ , collection := range transfer.Collections {
		store.ForEach( collection , func( envelope docs.Envelope ) bool {
			all[ envelope.Key() ] = fmt.Sprintf( "%v|%s|%v|%s" , envelope.HLC , envelope.Node , envelope.Deleted , envelope.Body )
			return true
		} )
	}
	result = fmt.Sprint( all )
	return
}

type body struct {
	Text string `json:"text"`
}

func TestExportImportRoundTripAndHubSeed( t *testing.T ) {
	source := station( t , "source" )
	for index := 0; index < 25; index++ {
		source.Put( "clients" , fmt.Sprintf( "c%02d" , index ) , body{ "family" } )
		source.Put( "visits" , fmt.Sprintf( "v%02d" , index ) , body{ "visit" } )
	}
	source.Delete( "clients" , "c03" )
	source.Put( "settings" , "check_in_cooloff_days" , map[string]int{ "value": 21 } )

	path := filepath.Join( t.TempDir() , "export.jsonl.gz" )
	result , err := transfer.Export( source , path , time.Now() )
	if err != nil || result.Records != 51 || result.Counts[ "clients" ] != 25 {
		t.Fatalf( "export: %+v %v" , result , err )
	}
	if _ , err := transfer.Export( source , path , time.Now() ); err == nil {
		t.Fatalf( "export must refuse to overwrite an existing file" )
	}

	restored := station( t , "restored" )
	imported , err := transfer.Import( restored , path )
	if err != nil || imported.Read != 51 || imported.Applied != 51 || imported.Header.Source != source.Node() {
		t.Fatalf( "import: %+v %v" , imported , err )
	}
	if snapshot( restored ) != snapshot( source ) {
		t.Fatalf( "a restored station must hold exactly what was exported" )
	}
	if again , _ := transfer.Import( restored , path ); again.Applied != 0 {
		t.Fatalf( "importing twice must change nothing: %+v" , again )
	}

	hub_db := testutil.OpenStore( t , filepath.Join( t.TempDir() , "hub.db" ) , hub.Buckets... )
	defer hub_db.Close()
	hub_store , _ := hub.Open( hub_db , hub.Limits{ MaxDocBytes: 1 << 20 , MaxFuture: 5 * time.Minute , PullPageMax: 10 } )
	seeded , err := hub_store.ImportFile( path , time.Now() )
	if err != nil || seeded.Stored != 51 {
		t.Fatalf( "hub import: %+v %v" , seeded , err )
	}
	// A fresh station pulling from the seeded hub ends up with the same data.
	fresh := station( t , "fresh" )
	since := uint64( 0 )
	for {
		page , err := hub_store.Pull( since , 0 , time.Now() )
		if err != nil {
			t.Fatal( err )
		}
		var incoming []docs.Envelope
		for _ , pulled := range page.Docs {
			incoming = append( incoming , pulled.Envelope )
		}
		fresh.ApplyPage( incoming , fmt.Sprint( page.Next ) )
		since = page.Next
		if page.HasMore == false {
			break
		}
	}
	if snapshot( fresh ) != snapshot( source ) {
		t.Fatalf( "a station pulling from a seeded hub must match the export" )
	}
}
