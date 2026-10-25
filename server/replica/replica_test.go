package replica

import (
	fmt "fmt"
	http "net/http"
	httptest "net/http/httptest"
	filepath "path/filepath"
	sync "sync"
	testing "testing"
	time "time"

	fiber "github.com/gofiber/fiber/v3"
	adaptor "github.com/gofiber/fiber/v3/middleware/adaptor"

	db "github.com/0187773933/MastersClosetTracker/server/db"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	hub "github.com/0187773933/MastersClosetTracker/server/hub"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
)

// testHub is a real hub behind a switch: it can be taken down, made to drop
// its answers after storing a push, or swapped for a fresh, empty one.
type testHub struct {
	t          *testing.T
	mu         sync.Mutex
	handler    http.Handler
	store      *hub.Store
	down       bool
	dropAnswer bool
	credential string
	server     *httptest.Server
	page_max   int
}

func newTestHub( t *testing.T , page_max int ) ( test_hub *testHub ) {
	test_hub = &testHub{ t: t , page_max: page_max }
	test_hub.replace()
	test_hub.server = httptest.NewServer( http.HandlerFunc( test_hub.serve ) )
	t.Cleanup( test_hub.server.Close )
	return
}

// replace swaps in a brand-new, empty hub database (a wiped hub), with a
// fresh station key the test hands to the stations.
func ( test_hub *testHub ) replace() {
	store := testutil.OpenStore( test_hub.t , filepath.Join( test_hub.t.TempDir() , "hub.db" ) , hub.Buckets... )
	test_hub.t.Cleanup( func() { store.Close() } )
	hub_store , err := hub.Open( store , hub.Limits{ MaxDocBytes: 1 << 20 , MaxFuture: 5 * time.Minute , PullPageMax: test_hub.page_max } )
	if err != nil {
		test_hub.t.Fatal( err )
	}
	app := fiber.New( fiber.Config{ Immutable: true , BodyLimit: 4 << 20 } )
	( &hub.Handlers{ Hub: hub_store , MaxPushBytes: 1 << 20 , Now: time.Now } ).Register( app )
	_ , credential , _ := hub_store.CreateKey( "test" , time.Now() )
	test_hub.mu.Lock()
	test_hub.store , test_hub.handler , test_hub.credential = hub_store , adaptor.FiberApp( app ) , credential
	test_hub.mu.Unlock()
}

func ( test_hub *testHub ) serve( writer http.ResponseWriter , request *http.Request ) {
	test_hub.mu.Lock()
	down , drop , handler := test_hub.down , test_hub.dropAnswer , test_hub.handler
	test_hub.mu.Unlock()
	if down {
		http.Error( writer , "down" , http.StatusBadGateway )
		return
	}
	if drop && request.URL.Path == "/sync/v1/push" {
		// Store it, then lose the answer on the way back.
		recorder := httptest.NewRecorder()
		handler.ServeHTTP( recorder , request )
		http.Error( writer , "connection reset" , http.StatusBadGateway )
		return
	}
	handler.ServeHTTP( writer , request )
}

type station struct {
	path    string
	store   *db.Store
	docs    *docs.Store
	replica *Replica
	offset  time.Duration
}

func ( item *station ) now() time.Time { return time.Now().Add( item.offset ) }

func newStation( t *testing.T , test_hub *testHub , name string , offset time.Duration ) ( item *station ) {
	item = &station{ path: filepath.Join( t.TempDir() , name+".db" ) , offset: offset }
	item.open( t , test_hub )
	t.Cleanup( func() { item.store.Close() } )
	return
}

func ( item *station ) open( t *testing.T , test_hub *testHub ) {
	item.store = testutil.OpenStore( t , item.path , docs.Buckets... )
	docs_store , err := docs.Open( item.store , item.now )
	if err != nil {
		t.Fatal( err )
	}
	item.docs = docs_store
	test_hub.mu.Lock()
	credential := test_hub.credential
	test_hub.mu.Unlock()
	item.replica = New( docs_store , Options{ HubURL: test_hub.server.URL , APIKey: credential , Station: "t" , Now: item.now } )
}

func ( item *station ) restart( t *testing.T , test_hub *testHub ) {
	item.store.Close()
	item.open( t , test_hub )
}

func ( item *station ) snapshot() ( result map[string]string ) {
	result = map[string]string{}
	for _ , collection := range []string{ "notes" , "visits" } {
		item.docs.ForEach( collection , func( envelope docs.Envelope ) bool {
			result[ envelope.Key() ] = fmt.Sprintf( "%v|%v|%s" , envelope.HLC , envelope.Deleted , envelope.Body )
			return true
		} )
	}
	return
}

func settle( t *testing.T , stations ...*station ) {
	t.Helper()
	for round := 0; round < 3; round++ {
		for _ , item := range stations {
			item.replica.SyncNow()
		}
	}
	want := stations[ 0 ].snapshot()
	for index , item := range stations {
		if got := item.snapshot(); fmt.Sprint( got ) != fmt.Sprint( want ) {
			t.Fatalf( "station %d diverged\n got: %v\nwant: %v" , index , got , want )
		}
		if pending := item.docs.PendingCount(); pending != 0 {
			t.Fatalf( "station %d still has %d pending: %+v" , index , pending , item.replica.StatusValue() )
		}
		if status := item.replica.StatusValue(); status.LastError != "" {
			t.Fatalf( "station %d error: %s" , index , status.LastError )
		}
	}
}

type note struct {
	Text string `json:"text"`
}

func TestBasicPropagationAndOfflineEditsConverge( t *testing.T ) {
	test_hub := newTestHub( t , 1000 )
	a , b , c := newStation( t , test_hub , "a" , 0 ) , newStation( t , test_hub , "b" , 0 ) , newStation( t , test_hub , "c" , 0 )
	a.docs.Put( "notes" , "n1" , note{ "from a" } )
	a.docs.Put( "visits" , "v1" , note{ "visit at a" } )
	settle( t , a , b , c )
	if got := b.snapshot(); len( got ) != 2 {
		t.Fatalf( "b should have both records: %v" , got )
	}

	// Hub down; both sides edit -- the same record and different ones.
	test_hub.mu.Lock()
	test_hub.down = true
	test_hub.mu.Unlock()
	a.docs.Put( "notes" , "n1" , note{ "a offline" } )
	time.Sleep( 3 * time.Millisecond ) // so "later" is a different millisecond
	b.docs.Put( "notes" , "n1" , note{ "b offline, later" } )
	b.docs.Put( "notes" , "n2" , note{ "b new" } )
	c.docs.Delete( "visits" , "v1" )
	if status := a.replica.SyncNow().( Status ); status.LastError == "" {
		t.Fatalf( "sync with the hub down should report an error" )
	}
	test_hub.mu.Lock()
	test_hub.down = false
	test_hub.mu.Unlock()
	settle( t , a , b , c )
	var body note
	got , _ := a.docs.Get( "notes" , "n1" )
	docs.Decode( got , &body )
	if body.Text != "b offline, later" {
		t.Fatalf( "the later edit should win, got %q" , body.Text )
	}
	if visit , _ := a.docs.Get( "visits" , "v1" ); visit.Deleted == false {
		t.Fatalf( "the delete should have propagated" )
	}
}

func TestEditAgainstDelete( t *testing.T ) {
	test_hub := newTestHub( t , 1000 )
	a , b := newStation( t , test_hub , "a" , 0 ) , newStation( t , test_hub , "b" , 0 )
	a.docs.Put( "notes" , "n1" , note{ "v1" } )
	settle( t , a , b )
	a.docs.Delete( "notes" , "n1" )
	time.Sleep( 3 * time.Millisecond )
	b.docs.Put( "notes" , "n1" , note{ "edited after the delete" } )
	settle( t , a , b )
	got , _ := a.docs.Get( "notes" , "n1" )
	if got.Deleted {
		t.Fatalf( "the later edit should win over the earlier delete" )
	}
}

func TestLostAcknowledgementIsResentHarmlessly( t *testing.T ) {
	test_hub := newTestHub( t , 1000 )
	a , b := newStation( t , test_hub , "a" , 0 ) , newStation( t , test_hub , "b" , 0 )
	a.docs.Put( "notes" , "n1" , note{ "stored but unacknowledged" } )
	test_hub.mu.Lock()
	test_hub.dropAnswer = true
	test_hub.mu.Unlock()
	a.replica.SyncNow()
	if a.docs.PendingCount() != 1 {
		t.Fatalf( "an unacknowledged push must stay queued" )
	}
	// The station is killed and restarted before it can retry.
	a.restart( t , test_hub )
	test_hub.mu.Lock()
	test_hub.dropAnswer = false
	test_hub.mu.Unlock()
	settle( t , a , b )
	if status , _ := test_hub.store.Status(); status.Counts[ "notes" ] != 1 {
		t.Fatalf( "the hub should hold the record once: %+v" , status )
	}
}

func TestWipedHubIsRebuiltFromStations( t *testing.T ) {
	test_hub := newTestHub( t , 1000 )
	a , b := newStation( t , test_hub , "a" , 0 ) , newStation( t , test_hub , "b" , 0 )
	for index := 0; index < 5; index++ {
		a.docs.Put( "notes" , fmt.Sprint( "a" , index ) , note{ "a" } )
		b.docs.Put( "notes" , fmt.Sprint( "b" , index ) , note{ "b" } )
	}
	settle( t , a , b )

	test_hub.replace()
	// The stations need the new hub's key; in real life an admin makes one.
	for _ , item := range []*station{ a , b } {
		item.replica.options.APIKey = test_hub.credential
	}
	settle( t , a , b )
	status , _ := test_hub.store.Status()
	if status.Counts[ "notes" ] != 10 {
		t.Fatalf( "the new hub should have every record back: %+v" , status )
	}
	if a.replica.StatusValue().HubEpoch != test_hub.store.Epoch() {
		t.Fatalf( "station did not adopt the new epoch" )
	}
}

func TestStationWithAFastClockIsCorrected( t *testing.T ) {
	test_hub := newTestHub( t , 1000 )
	fast := newStation( t , test_hub , "fast" , 10*time.Minute )
	normal := newStation( t , test_hub , "normal" , 0 )
	fast.docs.Put( "notes" , "n1" , note{ "written with a clock 10 minutes fast" } )
	settle( t , fast , normal )
	got , _ := normal.docs.Get( "notes" , "n1" )
	if ahead := time.Until( got.HLC.Time() ); ahead > time.Minute {
		t.Fatalf( "the record should have been re-stamped in the hub's time, is %v ahead" , ahead )
	}
	if fast.replica.StatusValue().ClockOffset > -int64( 9*time.Minute/time.Millisecond ) {
		t.Fatalf( "offset not measured: %+v" , fast.replica.StatusValue() )
	}
	// Later edits from the fast station keep working without refusals.
	fast.docs.Put( "notes" , "n1" , note{ "second edit" } )
	settle( t , fast , normal )
	var body note
	latest , _ := normal.docs.Get( "notes" , "n1" )
	if docs.Decode( latest , &body ); body.Text != "second edit" {
		t.Fatalf( "the fast station's later edit did not arrive: %q" , body.Text )
	}
}

func TestFreshStationCatchesUpOverManyPages( t *testing.T ) {
	test_hub := newTestHub( t , 7 )
	a := newStation( t , test_hub , "a" , 0 )
	for index := 0; index < 60; index++ {
		a.docs.Put( "visits" , fmt.Sprintf( "v%03d" , index ) , note{ "x" } )
	}
	settle( t , a )
	fresh := newStation( t , test_hub , "fresh" , 0 )
	settle( t , a , fresh )
	if status := fresh.replica.StatusValue(); status.CatchingUp || status.Cursor != status.MaxSeq || len( fresh.snapshot() ) != 60 {
		t.Fatalf( "fresh station did not catch up: %+v (%d records)" , status , len( fresh.snapshot() ) )
	}
}

func TestBackgroundLoopsPushOnWriteAndPollForOthers( t *testing.T ) {
	test_hub := newTestHub( t , 1000 )
	a , b := newStation( t , test_hub , "a" , 0 ) , newStation( t , test_hub , "b" , 0 )
	a.replica.options.Poll , b.replica.options.Poll = 100*time.Millisecond , 100*time.Millisecond
	a.replica.Start()
	b.replica.Start()
	defer a.replica.Stop()
	defer b.replica.Stop()
	a.docs.Put( "notes" , "live" , note{ "hello" } )
	deadline := time.Now().Add( 5 * time.Second )
	for time.Now().Before( deadline ) {
		if _ , err := b.docs.Get( "notes" , "live" ); err == nil {
			return
		}
		time.Sleep( 20 * time.Millisecond )
	}
	t.Fatalf( "b never received a's write: a=%+v b=%+v" , a.replica.StatusValue() , b.replica.StatusValue() )
}

func TestLargeFreshPull( t *testing.T ) {
	if testing.Short() {
		t.Skip( "timing test" )
	}
	test_hub := newTestHub( t , 1000 )
	const total = 50_000
	incoming := make( []docs.Envelope , 0 , 1000 )
	base := time.Now().Add( -time.Hour ).UnixMilli()
	for index := 0; index < total; index++ {
		envelope := docs.Envelope{ Collection: "visits" , ID: fmt.Sprintf( "v%06d" , index ) , Node: "seed" , Body: []byte( `{"client_id":"c","kind":"normal","items":{"tops":6}}` ) }
		envelope.HLC.Wall = base + int64( index )
		incoming = append( incoming , envelope )
		if len( incoming ) == cap( incoming ) {
			if _ , err := test_hub.store.Push( incoming , time.Now() ); err != nil {
				t.Fatal( err )
			}
			incoming = incoming[ :0 ]
		}
	}
	fresh := newStation( t , test_hub , "fresh" , 0 )
	started := time.Now()
	fresh.replica.SyncNow()
	elapsed := time.Since( started )
	count := 0
	fresh.docs.ForEach( "visits" , func( docs.Envelope ) bool { count++; return true } )
	if count != total {
		t.Fatalf( "pulled %d of %d" , count , total )
	}
	t.Logf( "fresh pull of %d records took %v" , total , elapsed.Round( time.Millisecond ) )
}
