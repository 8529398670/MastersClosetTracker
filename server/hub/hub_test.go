package hub

import (
	bytes "bytes"
	json "encoding/json"
	fmt "fmt"
	http "net/http"
	httptest "net/http/httptest"
	filepath "path/filepath"
	testing "testing"
	time "time"

	fiber "github.com/gofiber/fiber/v3"
	adaptor "github.com/gofiber/fiber/v3/middleware/adaptor"

	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
)

var now = time.UnixMilli( 1_700_000_000_000 )

func openHub( t *testing.T ) *Store {
	t.Helper()
	store := testutil.OpenStore( t , filepath.Join( t.TempDir() , "hub.db" ) , Buckets... )
	t.Cleanup( func() { store.Close() } )
	hub , err := Open( store , Limits{ MaxDocBytes: 1024 , MaxFuture: 5 * time.Minute , PullPageMax: 3 } )
	if err != nil {
		t.Fatal( err )
	}
	return hub
}

func doc( id string , wall int64 , body string ) syncproto.Envelope {
	return syncproto.Envelope{ Collection: "clients" , ID: id , HLC: hlc.Stamp{ Wall: wall } , Node: "n1" , Body: json.RawMessage( body ) }
}

func TestPushStatusesAndCompactedPagedPull( t *testing.T ) {
	hub := openHub( t )
	base := now.UnixMilli() - 60_000
	results , err := hub.Push( []syncproto.Envelope{
		doc( "a" , base , `{"v":1}` ),
		doc( "b" , base , `{"v":1}` ),
		doc( "c" , base , `{"v":1}` ),
		doc( "d" , base , `{"v":1}` ),
		doc( "future" , now.Add( 10*time.Minute ).UnixMilli() , `{}` ),
		{ Collection: "clients" , ID: "bad" , HLC: hlc.Stamp{ Wall: base } , Node: "n1" , Body: json.RawMessage( `not json` ) },
		doc( "huge" , base , `"`+string( bytes.Repeat( []byte( "x" ) , 2000 ) )+`"` ),
	} , now )
	if err != nil {
		t.Fatal( err )
	}
	statuses := ""
	for _ , result := range results {
		statuses += result.Status + " "
	}
	if statuses != "ok ok ok ok rejected_future rejected_invalid rejected_invalid " {
		t.Fatalf( "statuses: %s" , statuses )
	}

	// An older version of a is stale; a newer one moves a to the end of the feed.
	results , _ = hub.Push( []syncproto.Envelope{ doc( "a" , base-1 , `{"v":0}` ) , doc( "a" , base+1 , `{"v":2}` ) } , now )
	if results[ 0 ].Status != syncproto.StatusStale || results[ 1 ].Status != syncproto.StatusOK || results[ 1 ].Seq != 5 {
		t.Fatalf( "update results: %+v" , results )
	}

	var ids []string
	since := uint64( 0 )
	pages := 0
	for {
		page , err := hub.Pull( since , 0 , now )
		if err != nil {
			t.Fatal( err )
		}
		pages++
		for _ , pulled := range page.Docs {
			ids = append( ids , fmt.Sprintf( "%s@%d" , pulled.ID , pulled.Seq ) )
		}
		since = page.Next
		if page.MaxSeq != 5 || page.HubEpoch != hub.Epoch() {
			t.Fatalf( "page meta: %+v" , page )
		}
		if page.HasMore == false {
			break
		}
	}
	if fmt.Sprint( ids ) != "[b@2 c@3 d@4 a@5]" || pages != 2 {
		t.Fatalf( "feed should be compacted and paged: %v over %d pages" , ids , pages )
	}
	if empty , _ := hub.Pull( since , 0 , now ); len( empty.Docs ) != 0 || empty.Next != since {
		t.Fatalf( "pull past the end: %+v" , empty )
	}
}

func TestEpochSurvivesRestartButNotANewDatabase( t *testing.T ) {
	path := filepath.Join( t.TempDir() , "hub.db" )
	store := testutil.OpenStore( t , path , Buckets... )
	first , _ := Open( store , Limits{ PullPageMax: 10 } )
	store.Close()
	store = testutil.OpenStore( t , path , Buckets... )
	again , _ := Open( store , Limits{ PullPageMax: 10 } )
	store.Close()
	other := openHub( t )
	if first.Epoch() != again.Epoch() || first.Epoch() == other.Epoch() {
		t.Fatalf( "epochs: %s %s %s" , first.Epoch() , again.Epoch() , other.Epoch() )
	}
}

func TestKeys( t *testing.T ) {
	hub := openHub( t )
	info , credential , err := hub.CreateKey( "Front desk" , now )
	if err != nil {
		t.Fatal( err )
	}
	if _ , ok := hub.Authenticate( credential ); ok == false {
		t.Fatalf( "fresh key should authenticate" )
	}
	if _ , ok := hub.Authenticate( info.ID + ".wrong" ); ok {
		t.Fatalf( "wrong secret must fail" )
	}
	if _ , _ , err := hub.CreateKey( "  " , now ); err == nil {
		t.Fatalf( "a key needs a name" )
	}
	hub.RevokeKey( info.ID , now )
	if _ , ok := hub.Authenticate( credential ); ok {
		t.Fatalf( "revoked key must fail" )
	}
	if err := hub.DeleteKey( info.ID ); err != nil {
		t.Fatal( err )
	}
	if keys , _ := hub.ListKeys(); len( keys ) != 0 {
		t.Fatalf( "deleted key still listed" )
	}
}

func TestHTTPRequiresALiveKey( t *testing.T ) {
	hub := openHub( t )
	_ , credential , _ := hub.CreateKey( "Front desk" , now )
	app := fiber.New()
	( &Handlers{ Hub: hub , MaxPushBytes: 1 << 20 , RateMax: 0 , Now: func() time.Time { return now } } ).Register( app )
	server := httptest.NewServer( adaptor.FiberApp( app ) )
	defer server.Close()

	call := func( auth string ) int {
		request , _ := http.NewRequest( "GET" , server.URL+"/sync/v1/info" , nil )
		if auth != "" {
			request.Header.Set( "Authorization" , "Bearer "+auth )
		}
		response , err := http.DefaultClient.Do( request )
		if err != nil {
			t.Fatal( err )
		}
		response.Body.Close()
		return response.StatusCode
	}
	if code := call( "" ); code != 401 {
		t.Fatalf( "no key: %d" , code )
	}
	if code := call( credential ); code != 200 {
		t.Fatalf( "good key: %d" , code )
	}
	keys , _ := hub.ListKeys()
	hub.RevokeKey( keys[ 0 ].ID , now )
	if code := call( credential ); code != 401 {
		t.Fatalf( "revoked key: %d" , code )
	}
}
