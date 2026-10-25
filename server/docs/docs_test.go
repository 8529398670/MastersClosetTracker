package docs

import (
	errors "errors"
	fmt "fmt"
	rand "math/rand"
	filepath "path/filepath"
	sort "sort"
	testing "testing"
	time "time"

	db "github.com/0187773933/MastersClosetTracker/server/db"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
)

type fakeTime struct{ at time.Time }

func ( fake *fakeTime ) now() time.Time { return fake.at }

func openDocs( t *testing.T , path string , clock *fakeTime ) ( *Store , *db.Store ) {
	t.Helper()
	store := testutil.OpenStore( t , path , Buckets... )
	docs , err := Open( store , clock.now )
	if err != nil {
		t.Fatal( err )
	}
	return docs , store
}

type note struct {
	Text string `json:"text"`
}

func TestPutGetDeleteAndHooks( t *testing.T ) {
	clock := &fakeTime{ at: time.UnixMilli( 1_000_000 ) }
	docs , store := openDocs( t , filepath.Join( t.TempDir() , "a.db" ) , clock )
	defer store.Close()

	var seen []string
	docs.Hook( "notes" , func( current Envelope , previous *Envelope ) {
		seen = append( seen , fmt.Sprintf( "%s del=%v prev=%v" , current.ID , current.Deleted , previous != nil ) )
	} )
	kicked := 0
	docs.OnLocalWrite( func() { kicked++ } )

	first , err := docs.Put( "notes" , "n1" , note{ "hello" } )
	if err != nil {
		t.Fatal( err )
	}
	got , err := docs.Get( "notes" , "n1" )
	if err != nil || got.HLC != first.HLC {
		t.Fatalf( "get after put: %v %v" , got , err )
	}
	var body note
	if err := Decode( got , &body ); err != nil || body.Text != "hello" {
		t.Fatalf( "decode: %v %v" , body , err )
	}
	if _ , err := docs.Delete( "notes" , "n1" ); err != nil {
		t.Fatal( err )
	}
	if _ , err := docs.Delete( "notes" , "n1" ); err != nil {
		t.Fatalf( "second delete should be a no-op, got %v" , err )
	}
	if _ , err := docs.Delete( "notes" , "missing" ); errors.Is( err , ErrNotFound ) == false {
		t.Fatalf( "delete of unknown record: %v" , err )
	}
	if len( seen ) != 2 || seen[ 0 ] != "n1 del=false prev=false" || seen[ 1 ] != "n1 del=true prev=true" {
		t.Fatalf( "hooks saw %v" , seen )
	}
	if kicked != 2 {
		t.Fatalf( "local-write callback ran %d times, want 2" , kicked )
	}
	if docs.PendingCount() != 1 {
		t.Fatalf( "outbox should coalesce to one entry, has %d" , docs.PendingCount() )
	}
}

func TestPutIfRefusesAStaleVersion( t *testing.T ) {
	clock := &fakeTime{ at: time.UnixMilli( 1_000_000 ) }
	docs , store := openDocs( t , filepath.Join( t.TempDir() , "a.db" ) , clock )
	defer store.Close()

	v1 , _ := docs.Put( "notes" , "n1" , note{ "one" } )
	v2 , err := docs.PutIf( "notes" , "n1" , note{ "two" } , v1.HLC )
	if err != nil {
		t.Fatal( err )
	}
	if _ , err := docs.PutIf( "notes" , "n1" , note{ "stale tab" } , v1.HLC ); errors.Is( err , ErrConflict ) == false {
		t.Fatalf( "stale write should conflict, got %v" , err )
	}
	docs.Delete( "notes" , "n1" )
	if _ , err := docs.PutIf( "notes" , "n1" , note{ "after delete" } , v2.HLC ); errors.Is( err , ErrConflict ) == false {
		t.Fatalf( "write to a deleted record should conflict, got %v" , err )
	}
}

func TestOutboxMarkPushedKeepsANewerEdit( t *testing.T ) {
	clock := &fakeTime{ at: time.UnixMilli( 1_000_000 ) }
	docs , store := openDocs( t , filepath.Join( t.TempDir() , "a.db" ) , clock )
	defer store.Close()

	docs.Put( "notes" , "n1" , note{ "one" } )
	sent , _ := docs.Pending( 10 , 1<<20 )
	docs.Put( "notes" , "n1" , note{ "edited while pushing" } )
	docs.MarkPushed( sent )
	if docs.PendingCount() != 1 {
		t.Fatalf( "the newer edit must stay queued" )
	}
	again , _ := docs.Pending( 10 , 1<<20 )
	docs.MarkPushed( again )
	if docs.PendingCount() != 0 {
		t.Fatalf( "outbox should be empty, has %d" , docs.PendingCount() )
	}
}

func TestApplyIsIdempotentAndDropsALosingOutboxEntry( t *testing.T ) {
	clock := &fakeTime{ at: time.UnixMilli( 1_000_000 ) }
	docs , store := openDocs( t , filepath.Join( t.TempDir() , "a.db" ) , clock )
	defer store.Close()

	local , _ := docs.Put( "notes" , "n1" , note{ "local" } )
	remote := Envelope{ Collection: "notes" , ID: "n1" , HLC: hlc.Stamp{ Wall: local.HLC.Wall + 1000 } , Node: "zzzz" , Body: []byte( `{"text":"remote"}` ) }
	applied , _ , err := docs.Apply( []Envelope{ remote , remote } )
	if err != nil || applied != 1 {
		t.Fatalf( "apply: applied=%d err=%v" , applied , err )
	}
	if docs.PendingCount() != 0 {
		t.Fatalf( "losing local version should leave the outbox" )
	}
	applied , _ , _ = docs.Apply( []Envelope{ remote } )
	if applied != 0 {
		t.Fatalf( "re-applying the same version should be a no-op" )
	}
	older := remote
	older.HLC = hlc.Stamp{ Wall: 1 }
	older.Body = []byte( `{"text":"ancient"}` )
	applied , _ , _ = docs.Apply( []Envelope{ older } )
	if applied != 0 {
		t.Fatalf( "an older version must not replace a newer one" )
	}
	next , _ := docs.Put( "notes" , "n1" , note{ "after remote" } )
	if next.HLC.After( remote.HLC ) == false {
		t.Fatalf( "a local edit after seeing a remote one must sort after it" )
	}
	invalid := Envelope{ Collection: "Bad Name" , ID: "x" , HLC: hlc.Stamp{ Wall: 5 } , Node: "n" , Body: []byte( `{}` ) }
	if _ , skipped , _ := docs.Apply( []Envelope{ invalid } ); skipped != 1 {
		t.Fatalf( "invalid envelope should be skipped" )
	}
}

func TestClockAndOutboxSurviveRestart( t *testing.T ) {
	clock := &fakeTime{ at: time.UnixMilli( 5_000_000 ) }
	path := filepath.Join( t.TempDir() , "a.db" )
	docs , store := openDocs( t , path , clock )
	written , _ := docs.Put( "notes" , "n1" , note{ "one" } )
	node := docs.Node()
	store.Close()

	clock.at = clock.at.Add( -time.Hour ) // the machine's clock went back
	docs , store = openDocs( t , path , clock )
	defer store.Close()
	if docs.Node() != node {
		t.Fatalf( "node id changed across restart" )
	}
	if docs.PendingCount() != 1 {
		t.Fatalf( "outbox lost across restart" )
	}
	next , _ := docs.Put( "notes" , "n2" , note{ "two" } )
	if next.HLC.After( written.HLC ) == false {
		t.Fatalf( "stamp after restart (%v) must follow the persisted high-water mark (%v)" , next.HLC , written.HLC )
	}
}

func TestForEachStaysInsideItsCollection( t *testing.T ) {
	clock := &fakeTime{ at: time.UnixMilli( 1_000_000 ) }
	docs , store := openDocs( t , filepath.Join( t.TempDir() , "a.db" ) , clock )
	defer store.Close()
	docs.Put( "notes" , "a" , note{ "a" } )
	docs.Put( "notesx" , "b" , note{ "b" } )
	docs.Put( "other" , "c" , note{ "c" } )
	var ids []string
	docs.ForEach( "notes" , func( envelope Envelope ) bool {
		ids = append( ids , envelope.ID )
		return true
	} )
	if len( ids ) != 1 || ids[ 0 ] != "a" {
		t.Fatalf( "ForEach(notes) = %v" , ids )
	}
}

// ---------------------------------------------------------------------------
// randomized convergence
// ---------------------------------------------------------------------------

// relay is a minimal in-memory hub with the same rule the real one uses:
// keep the winning version per key, number every accepted change, compact
// the feed to one entry per key.
type relay struct {
	docs map[string]syncproto.PulledDoc
	seq  uint64
}

func ( relay *relay ) push( incoming []Envelope ) {
	for _ , envelope := range incoming {
		current , exists := relay.docs[ envelope.Key() ]
		if exists && syncproto.Wins( envelope , current.Envelope ) == false {
			continue
		}
		relay.seq++
		relay.docs[ envelope.Key() ] = syncproto.PulledDoc{ Envelope: envelope , Seq: relay.seq }
	}
}

func ( relay *relay ) pull( since uint64 , limit int ) ( page []syncproto.PulledDoc , next uint64 , more bool ) {
	var all []syncproto.PulledDoc
	for _ , doc := range relay.docs {
		if doc.Seq > since {
			all = append( all , doc )
		}
	}
	sort.Slice( all , func( i , j int ) bool { return all[ i ].Seq < all[ j ].Seq } )
	next = since
	if len( all ) > limit {
		all , more = all[ :limit ] , true
	}
	if len( all ) > 0 {
		next = all[ len( all )-1 ].Seq
	}
	page = all
	return
}

type simNode struct {
	path   string
	clock  *fakeTime
	docs   *Store
	store  *db.Store
	cursor uint64
}

func snapshot( t *testing.T , docs *Store ) ( result map[string]string ) {
	result = map[string]string{}
	for _ , collection := range []string{ "notes" , "tags" } {
		docs.ForEach( collection , func( envelope Envelope ) bool {
			result[ envelope.Key() ] = fmt.Sprintf( "%v|%s|%v|%s" , envelope.HLC , envelope.Node , envelope.Deleted , envelope.Body )
			return true
		} )
	}
	return
}

func TestRandomizedThreeNodeConvergence( t *testing.T ) {
	for seed := int64( 1 ); seed <= 8; seed++ {
		t.Run( fmt.Sprintf( "seed%d" , seed ) , func( t *testing.T ) {
			runConvergence( t , seed )
		} )
	}
}

func runConvergence( t *testing.T , seed int64 ) {
	random := rand.New( rand.NewSource( seed ) )
	dir := t.TempDir()
	hub := &relay{ docs: map[string]syncproto.PulledDoc{} }
	base := time.UnixMilli( 1_700_000_000_000 )
	nodes := make( []*simNode , 3 )
	for index := range nodes {
		node := &simNode{ path: filepath.Join( dir , fmt.Sprintf( "n%d.db" , index ) ) , clock: &fakeTime{ at: base.Add( time.Duration( index*7 ) * time.Second ) } }
		node.docs , node.store = openDocs( t , node.path , node.clock )
		nodes[ index ] = node
	}
	defer func() {
		for _ , node := range nodes {
			node.store.Close()
		}
	}()

	pushOnce := func( node *simNode , reliable bool ) {
		pending , _ := node.docs.Pending( 1+random.Intn( 5 ) , 1<<20 )
		if len( pending ) == 0 { return }
		if reliable == false && random.Intn( 4 ) == 0 {
			return // lost on the way: never acknowledged, stays queued
		}
		hub.push( pending )
		if reliable == false && random.Intn( 4 ) == 0 {
			hub.push( pending ) // delivered twice
		}
		if reliable || random.Intn( 5 ) != 0 {
			node.docs.MarkPushed( pending ) // or the ack is lost: pushed again later
		}
	}
	pullOnce := func( node *simNode , reliable bool ) ( more bool ) {
		page , next , more := hub.pull( node.cursor , 1+random.Intn( 4 ) )
		incoming := make( []Envelope , 0 , len( page ) )
		for _ , doc := range page {
			incoming = append( incoming , doc.Envelope )
		}
		if reliable == false {
			random.Shuffle( len( incoming ) , func( i , j int ) { incoming[ i ] , incoming[ j ] = incoming[ j ] , incoming[ i ] } )
			if len( incoming ) > 0 && random.Intn( 3 ) == 0 {
				incoming = append( incoming , incoming[ 0 ] )
			}
		}
		if _ , _ , err := node.docs.ApplyPage( incoming , fmt.Sprint( next ) ); err != nil {
			t.Fatal( err )
		}
		node.cursor = next
		return
	}

	keys := []string{ "a" , "b" , "c" , "d" , "e" }
	for step := 0; step < 400; step++ {
		node := nodes[ random.Intn( len( nodes ) ) ]
		node.clock.at = node.clock.at.Add( time.Duration( random.Intn( 3000 ) ) * time.Millisecond )
		switch roll := random.Intn( 100 ); {
		case roll < 40:
			collection := []string{ "notes" , "tags" }[ random.Intn( 2 ) ]
			node.docs.Put( collection , keys[ random.Intn( len( keys ) ) ] , note{ fmt.Sprintf( "s%d" , step ) } )
		case roll < 50:
			node.docs.Delete( "notes" , keys[ random.Intn( len( keys ) ) ] )
		case roll < 70:
			pushOnce( node , false )
		case roll < 90:
			pullOnce( node , false )
		case roll < 95:
			// restart: close and reopen from disk
			node.store.Close()
			node.docs , node.store = openDocs( t , node.path , node.clock )
		default:
			// the machine's clock steps back a few minutes
			node.clock.at = node.clock.at.Add( -time.Duration( 1+random.Intn( 5 ) ) * time.Minute )
		}
	}

	// Heal the network: everyone pushes everything, then pulls everything.
	for round := 0; round < 3; round++ {
		for _ , node := range nodes {
			for node.docs.PendingCount() > 0 {
				pushOnce( node , true )
			}
		}
		for _ , node := range nodes {
			for pullOnce( node , true ) {
			}
		}
	}

	want := map[string]string{}
	for key , doc := range hub.docs {
		want[ key ] = fmt.Sprintf( "%v|%s|%v|%s" , doc.HLC , doc.Node , doc.Deleted , doc.Body )
	}
	for index , node := range nodes {
		got := snapshot( t , node.docs )
		if fmt.Sprint( got ) != fmt.Sprint( want ) {
			t.Fatalf( "node %d diverged from the hub\n got: %v\nwant: %v" , index , got , want )
		}
		if node.docs.PendingCount() != 0 {
			t.Fatalf( "node %d still has %d pending" , index , node.docs.PendingCount() )
		}
	}
}
