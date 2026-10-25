// Package docs is the synced document store: the one way a station writes
// anything that should reach the other stations.
//
// Every record is an envelope (see server/syncproto) -- a collection, an id,
// a clock stamp, the node that wrote it, and a JSON body. A local write
// stamps the record and, in the same bolt transaction, notes it in the
// outbox; the replica (server/replica) pushes the outbox to the hub and
// applies what other stations sent. Either way, a record only ever moves to
// a version that wins under syncproto.Wins, so every station converges on
// the same data no matter what order changes arrive in.
//
// Deletes are tombstones: the envelope stays, marked deleted, so the
// deletion itself syncs. They are never purged -- a station that was offline
// for a month must not bring a deleted record back.
//
// Nothing here knows what a client or a visit is. Packages that do register
// hooks to keep their in-memory views current (server/index), and those hooks
// run for local writes and remote ones alike.
package docs

import (
	hex "encoding/hex"
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	strings "strings"
	sync "sync"
	time "time"

	db "github.com/0187773933/MastersClosetTracker/server/db"
	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
)

// Buckets this package owns. Pass Buckets to db.Open.
const (
	BucketDocs   = "docs"       // collection\x00id -> Envelope
	BucketOutbox = "doc_outbox" // collection\x00id -> stamp of the version still to push
	BucketMeta   = "doc_meta"   // node id, clock high-water mark, replica cursor, hub epoch
)

var Buckets = []string{ BucketDocs , BucketOutbox , BucketMeta }

// Meta keys. The replica's own keys live here too so the cursor can advance
// in the same transaction as the records it covers.
const (
	MetaNodeID   = "node_id"
	MetaClock    = "clock"
	MetaCursor   = "replica_cursor"
	MetaHubEpoch = "replica_hub_epoch"
)

var (
	ErrNotFound = errors.New( "docs: not found" )

	// ErrConflict is a write that expected a particular version and found
	// another -- someone else's edit (here or synced in) landed first.
	ErrConflict = errors.New( "docs: record changed since it was read" )
)

type Envelope = syncproto.Envelope

// Hook sees every change to one collection, after it is committed. previous
// is nil for a record this station had never seen.
type Hook func( current Envelope , previous *Envelope )

type Store struct {
	db    *db.Store
	clock *hlc.Clock
	node  string

	// writes serialises each write together with its hooks, so hooks see
	// changes in commit order and an index can never be left holding the
	// older of two versions.
	writes sync.Mutex

	hooks_mu sync.RWMutex
	hooks    map[string][]Hook

	local_mu sync.RWMutex
	on_local func()
}

// Open loads (or on first boot creates) this database's node id and clock.
// The node id belongs to the database file, not the machine or the config:
// a restored or copied database keeps its own, and a fresh one never reuses
// another's.
func Open( store *db.Store , now func() time.Time ) ( docs *Store , err error ) {
	var node string
	var last hlc.Stamp
	err = store.Write( func( tx *db.Tx ) ( tx_err error ) {
		get_err := tx.Get( BucketMeta , []byte( MetaNodeID ) , &node )
		if errors.Is( get_err , db.ErrNotFound ) {
			node = hex.EncodeToString( encryption.GenerateRandomBytes( 8 ) )
			tx_err = tx.Put( BucketMeta , []byte( MetaNodeID ) , node )
			if tx_err != nil { return }
		} else if get_err != nil {
			tx_err = get_err
			return
		}
		var stamp string
		get_err = tx.Get( BucketMeta , []byte( MetaClock ) , &stamp )
		if get_err == nil {
			last , tx_err = hlc.Parse( stamp )
		} else if errors.Is( get_err , db.ErrNotFound ) == false {
			tx_err = get_err
		}
		return
	} )
	if err != nil { return }
	docs = &Store{
		db:    store,
		clock: hlc.New( last , now ),
		node:  node,
		hooks: map[string][]Hook{},
	}
	return
}

func ( docs *Store ) Node() ( node string ) {
	node = docs.node
	return
}

func ( docs *Store ) Clock() ( clock *hlc.Clock ) {
	clock = docs.clock
	return
}

// OnLocalWrite registers fn to be told (without blocking the write) that
// there is something new to push. The replica uses it to push at once
// instead of on its next poll.
func ( docs *Store ) OnLocalWrite( fn func() ) {
	docs.local_mu.Lock()
	docs.on_local = fn
	docs.local_mu.Unlock()
}

// Hook registers fn for every change to collection.
func ( docs *Store ) Hook( collection string , fn Hook ) {
	docs.hooks_mu.Lock()
	docs.hooks[ collection ] = append( docs.hooks[ collection ] , fn )
	docs.hooks_mu.Unlock()
}

func ( docs *Store ) fire( current Envelope , previous *Envelope ) {
	docs.hooks_mu.RLock()
	hooks := docs.hooks[ current.Collection ]
	docs.hooks_mu.RUnlock()
	for _ , fn := range hooks {
		fn( current , previous )
	}
}

func ( docs *Store ) kick() {
	docs.local_mu.RLock()
	fn := docs.on_local
	docs.local_mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func key( collection string , id string ) ( result []byte ) {
	result = []byte( collection + "\x00" + id )
	return
}

// ---------------------------------------------------------------------------
// local writes
// ---------------------------------------------------------------------------

// Put writes body (any JSON-encodable value) as the new version of a record.
func ( docs *Store ) Put( collection string , id string , body any ) ( envelope Envelope , err error ) {
	envelope , err = docs.write( collection , id , false , body , nil )
	return
}

// PutIf is Put only if the record is still at version expect -- the stamp
// the caller read. It returns ErrConflict otherwise, including when the
// record was deleted in between.
func ( docs *Store ) PutIf( collection string , id string , body any , expect hlc.Stamp ) ( envelope Envelope , err error ) {
	envelope , err = docs.write( collection , id , false , body , &expect )
	return
}

// Delete writes a tombstone. Deleting a record that does not exist is
// ErrNotFound; deleting one already deleted is a no-op.
func ( docs *Store ) Delete( collection string , id string ) ( envelope Envelope , err error ) {
	envelope , err = docs.write( collection , id , true , nil , nil )
	return
}

func ( docs *Store ) write( collection string , id string , deleted bool , body any , expect *hlc.Stamp ) ( envelope Envelope , err error ) {
	if syncproto.ValidCollection( collection ) == false || syncproto.ValidID( id ) == false {
		err = fmt.Errorf( "docs: invalid collection %q or id %q" , collection , id )
		return
	}
	var raw json.RawMessage
	if deleted == false {
		if raw , err = json.Marshal( body ); err != nil { return }
	}
	// The envelope outlives this call (hooks keep it in memory), so it must
	// own its strings rather than share a caller's buffer.
	collection , id = strings.Clone( collection ) , strings.Clone( id )

	docs.writes.Lock()
	defer docs.writes.Unlock()

	var previous *Envelope
	unchanged := false
	err = docs.db.Write( func( tx *db.Tx ) ( tx_err error ) {
		var existing Envelope
		get_err := tx.Get( BucketDocs , key( collection , id ) , &existing )
		switch {
		case get_err == nil:
			previous = &existing
		case errors.Is( get_err , db.ErrNotFound ):
		default:
			tx_err = get_err
			return
		}
		if expect != nil && ( previous == nil || previous.Deleted || previous.HLC != *expect ) {
			tx_err = ErrConflict
			return
		}
		if deleted && previous == nil {
			tx_err = ErrNotFound
			return
		}
		if deleted && previous.Deleted {
			unchanged = true
			envelope = *previous
			return
		}
		// A record's next version must sort after its current one even if
		// this machine's clock is behind the machine that wrote it.
		if previous != nil {
			docs.clock.Observe( previous.HLC )
		}
		envelope = Envelope{
			Collection: collection,
			ID:         id,
			HLC:        docs.clock.Now(),
			Node:       docs.node,
			Deleted:    deleted,
			Body:       raw,
		}
		if tx_err = tx.Put( BucketDocs , key( collection , id ) , envelope ); tx_err != nil { return }
		if tx_err = tx.Put( BucketOutbox , key( collection , id ) , envelope.HLC.String() ); tx_err != nil { return }
		tx_err = tx.Put( BucketMeta , []byte( MetaClock ) , docs.clock.Last().String() )
		return
	} )
	if err != nil || unchanged { return }
	docs.fire( envelope , previous )
	docs.kick()
	return
}

// Restamp gives records new stamps without changing their bodies. The
// replica uses it when the hub refused a stamp as too far in the future:
// once the clock offset is corrected, the same edit is re-sent stamped in
// the hub's timeline.
func ( docs *Store ) Restamp( keys []Envelope ) ( err error ) {
	docs.writes.Lock()
	defer docs.writes.Unlock()
	var changed [][ 2 ]Envelope
	err = docs.db.Write( func( tx *db.Tx ) ( tx_err error ) {
		for _ , wanted := range keys {
			var existing Envelope
			if get_err := tx.Get( BucketDocs , key( wanted.Collection , wanted.ID ) , &existing ); get_err != nil {
				continue
			}
			if existing.HLC != wanted.HLC {
				// Edited again since it was sent; that version is already
				// stamped with the corrected clock.
				continue
			}
			restamped := existing
			restamped.HLC = docs.clock.Now()
			restamped.Node = docs.node
			if tx_err = tx.Put( BucketDocs , key( existing.Collection , existing.ID ) , restamped ); tx_err != nil { return }
			if tx_err = tx.Put( BucketOutbox , key( existing.Collection , existing.ID ) , restamped.HLC.String() ); tx_err != nil { return }
			changed = append( changed , [ 2 ]Envelope{ restamped , existing } )
		}
		tx_err = tx.Put( BucketMeta , []byte( MetaClock ) , docs.clock.Last().String() )
		return
	} )
	if err != nil { return }
	for _ , pair := range changed {
		previous := pair[ 1 ]
		docs.fire( pair[ 0 ] , &previous )
	}
	if len( changed ) > 0 {
		docs.kick()
	}
	return
}

// ---------------------------------------------------------------------------
// reads
// ---------------------------------------------------------------------------

// Get returns the current version, tombstone included. ErrNotFound means
// this station has never seen the record.
func ( docs *Store ) Get( collection string , id string ) ( envelope Envelope , err error ) {
	err = docs.db.View( func( tx *db.Tx ) ( tx_err error ) {
		tx_err = tx.Get( BucketDocs , key( collection , id ) , &envelope )
		return
	} )
	if errors.Is( err , db.ErrNotFound ) {
		err = ErrNotFound
	}
	return
}

// ForEach visits every record of a collection, tombstones included, in id
// order. Return false to stop.
func ( docs *Store ) ForEach( collection string , visit func( envelope Envelope ) bool ) ( err error ) {
	prefix := collection + "\x00"
	err = docs.db.View( func( tx *db.Tx ) ( tx_err error ) {
		tx_err = tx.ForEachFrom( BucketDocs , []byte( prefix ) , func() any { return &Envelope{} } , func( raw_key []byte , item any ) ( more bool ) {
			if strings.HasPrefix( string( raw_key ) , prefix ) == false {
				return
			}
			more = visit( *item.( *Envelope ) )
			return
		} )
		return
	} )
	return
}

// Decode unmarshals a record's body.
func Decode( envelope Envelope , out any ) ( err error ) {
	if envelope.Deleted || len( envelope.Body ) == 0 {
		err = ErrNotFound
		return
	}
	err = json.Unmarshal( envelope.Body , out )
	return
}

// ---------------------------------------------------------------------------
// remote changes
// ---------------------------------------------------------------------------

// ApplyPage stores every incoming version that wins over what this station
// has, and records cursor_value under MetaCursor in the same transaction --
// so a crash can never leave the cursor past records that were not applied,
// or before records that were. A cursor_value of "" leaves it alone.
//
// A local outbox entry whose version just lost is dropped: the hub already
// has something newer, so there is nothing left to push.
func ( docs *Store ) ApplyPage( incoming []Envelope , cursor_value string ) ( applied int , skipped int , err error ) {
	applied , skipped , err = docs.apply( incoming , cursor_value , false )
	return
}

// Import is Apply for records that arrive from a file rather than from the
// hub -- an export being restored, or the old system's data. They are
// applied by the same rule, and every one that was applied is also queued
// for the hub, which has never seen them.
func ( docs *Store ) Import( incoming []Envelope ) ( applied int , skipped int , err error ) {
	applied , skipped , err = docs.apply( incoming , "" , true )
	if err == nil && applied > 0 {
		docs.kick()
	}
	return
}

func ( docs *Store ) apply( incoming []Envelope , cursor_value string , queue bool ) ( applied int , skipped int , err error ) {
	docs.writes.Lock()
	defer docs.writes.Unlock()
	var changed [][ 2 ]*Envelope
	err = docs.db.Write( func( tx *db.Tx ) ( tx_err error ) {
		for index := range incoming {
			remote := incoming[ index ]
			if remote.Valid() == false {
				skipped++
				continue
			}
			docs.clock.Observe( remote.HLC )
			var previous *Envelope
			var existing Envelope
			get_err := tx.Get( BucketDocs , key( remote.Collection , remote.ID ) , &existing )
			switch {
			case get_err == nil:
				if syncproto.Wins( remote , existing ) == false {
					continue
				}
				previous = &existing
			case errors.Is( get_err , db.ErrNotFound ):
			default:
				tx_err = get_err
				return
			}
			if tx_err = tx.Put( BucketDocs , key( remote.Collection , remote.ID ) , remote ); tx_err != nil { return }
			if queue {
				tx_err = tx.Put( BucketOutbox , key( remote.Collection , remote.ID ) , remote.HLC.String() )
			} else {
				tx_err = tx.Delete( BucketOutbox , key( remote.Collection , remote.ID ) )
			}
			if tx_err != nil { return }
			current := remote
			changed = append( changed , [ 2 ]*Envelope{ &current , previous } )
			applied++
		}
		if tx_err = tx.Put( BucketMeta , []byte( MetaClock ) , docs.clock.Last().String() ); tx_err != nil { return }
		if cursor_value != "" {
			tx_err = tx.Put( BucketMeta , []byte( MetaCursor ) , cursor_value )
		}
		return
	} )
	if err != nil {
		applied = 0
		return
	}
	for _ , pair := range changed {
		docs.fire( *pair[ 0 ] , pair[ 1 ] )
	}
	return
}

// Apply is ApplyPage without a cursor -- an import, or a test.
func ( docs *Store ) Apply( incoming []Envelope ) ( applied int , skipped int , err error ) {
	applied , skipped , err = docs.ApplyPage( incoming , "" )
	return
}

// ---------------------------------------------------------------------------
// the outbox
// ---------------------------------------------------------------------------

// Pending returns up to limit records still to push, current versions, with
// their total encoded size kept under max_bytes (at least one is always
// returned when any are pending, so one large record cannot wedge the queue
// -- the hub will refuse it if it is over its own limit).
func ( docs *Store ) Pending( limit int , max_bytes int ) ( pending []Envelope , err error ) {
	size := 0
	err = docs.db.View( func( tx *db.Tx ) ( tx_err error ) {
		tx_err = tx.ForEach( BucketOutbox , func() any { var stamp string; return &stamp } , func( raw_key []byte , _ any ) ( more bool ) {
			var envelope Envelope
			if get_err := tx.Get( BucketDocs , raw_key , &envelope ); get_err != nil {
				return true
			}
			encoded := len( envelope.Body ) + len( envelope.ID ) + 128
			if len( pending ) > 0 && size+encoded > max_bytes {
				return false
			}
			size += encoded
			pending = append( pending , envelope )
			more = len( pending ) < limit
			return
		} )
		return
	} )
	return
}

// MarkPushed clears outbox entries the hub has accepted (or already had
// something newer for) -- but only where the entry still names the version
// that was sent. An edit made while the push was in flight stays queued.
func ( docs *Store ) MarkPushed( sent []Envelope ) ( err error ) {
	err = docs.db.Write( func( tx *db.Tx ) ( tx_err error ) {
		for _ , envelope := range sent {
			var stamp string
			if get_err := tx.Get( BucketOutbox , key( envelope.Collection , envelope.ID ) , &stamp ); get_err != nil {
				continue
			}
			if stamp != envelope.HLC.String() {
				continue
			}
			if tx_err = tx.Delete( BucketOutbox , key( envelope.Collection , envelope.ID ) ); tx_err != nil { return }
		}
		return
	} )
	return
}

// PendingCount is how many records are waiting to be pushed.
func ( docs *Store ) PendingCount() ( count int ) {
	docs.db.View( func( tx *db.Tx ) ( tx_err error ) {
		count = tx.Count( BucketOutbox )
		return
	} )
	return
}

// RequeueAll puts every record back in the outbox. A station does this when
// the hub's epoch changes -- a wiped or replaced hub -- so the new hub ends
// up with everything this station knows.
func ( docs *Store ) RequeueAll() ( queued int , err error ) {
	err = docs.db.Write( func( tx *db.Tx ) ( tx_err error ) {
		var all []Envelope
		tx_err = tx.ForEach( BucketDocs , func() any { return &Envelope{} } , func( _ []byte , item any ) bool {
			all = append( all , *item.( *Envelope ) )
			return true
		} )
		if tx_err != nil { return }
		for _ , envelope := range all {
			if tx_err = tx.Put( BucketOutbox , key( envelope.Collection , envelope.ID ) , envelope.HLC.String() ); tx_err != nil { return }
		}
		queued = len( all )
		return
	} )
	if err == nil && queued > 0 {
		docs.kick()
	}
	return
}

// ---------------------------------------------------------------------------
// meta
// ---------------------------------------------------------------------------

func ( docs *Store ) GetMeta( name string ) ( value string ) {
	docs.db.View( func( tx *db.Tx ) ( tx_err error ) {
		tx.Get( BucketMeta , []byte( name ) , &value )
		return
	} )
	return
}

func ( docs *Store ) SetMeta( name string , value string ) ( err error ) {
	err = docs.db.Write( func( tx *db.Tx ) ( tx_err error ) {
		tx_err = tx.Put( BucketMeta , []byte( name ) , value )
		return
	} )
	return
}

// Counts is the number of live records and tombstones per collection, for
// status pages and import reports.
func ( docs *Store ) Counts() ( live map[string]int , deleted map[string]int , err error ) {
	live , deleted = map[string]int{} , map[string]int{}
	err = docs.db.View( func( tx *db.Tx ) ( tx_err error ) {
		tx_err = tx.ForEach( BucketDocs , func() any { return &Envelope{} } , func( _ []byte , item any ) bool {
			envelope := item.( *Envelope )
			if envelope.Deleted {
				deleted[ envelope.Collection ]++
			} else {
				live[ envelope.Collection ]++
			}
			return true
		} )
		return
	} )
	return
}
