// Package hub is the relay every station syncs through.
//
// It keeps the winning version of each record (by syncproto.Wins, judged
// from the envelope's stamp alone -- the hub never reads a record's body)
// and numbers every change with its own sequence. A station pushes what it
// has written and pulls everything numbered after the last change it saw.
//
// The change feed is compacted: when a record changes, its old feed entry is
// removed, so a fresh station pulling from zero gets each record once, at its
// latest version, however often it was edited.
//
// The sequence belongs to this hub database. Each database also has a random
// epoch; a station that sees the epoch change knows the hub was replaced
// (wiped, restored, moved) and starts over -- re-pulling from zero and
// re-pushing everything it has. That is also the whole recovery plan for a
// lost hub: start an empty one and let the stations repopulate it.
package hub

import (
	binary "encoding/binary"
	errors "errors"
	hex "encoding/hex"
	time "time"

	db "github.com/0187773933/MastersClosetTracker/server/db"
	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
)

const (
	BucketDocs = "hub_docs" // collection\x00id -> stored
	BucketFeed = "hub_feed" // big-endian seq -> collection\x00id
	BucketMeta = "hub_meta" // epoch
	BucketKeys = "hub_keys" // key id -> Key

	metaEpoch = "epoch"
)

var Buckets = []string{ BucketDocs , BucketFeed , BucketMeta , BucketKeys }

// stored is a record as the hub keeps it.
type stored struct {
	Envelope syncproto.Envelope `json:"envelope"`
	Seq      uint64             `json:"seq"`
}

// Limits bound what a push may contain.
type Limits struct {
	MaxDocBytes int
	MaxFuture   time.Duration
	PullPageMax int
}

type Store struct {
	db     *db.Store
	epoch  string
	limits Limits
}

func Open( store *db.Store , limits Limits ) ( hub *Store , err error ) {
	var epoch string
	err = store.Write( func( tx *db.Tx ) ( tx_err error ) {
		get_err := tx.Get( BucketMeta , []byte( metaEpoch ) , &epoch )
		if errors.Is( get_err , db.ErrNotFound ) {
			epoch = hex.EncodeToString( encryption.GenerateRandomBytes( 8 ) )
			tx_err = tx.Put( BucketMeta , []byte( metaEpoch ) , epoch )
			return
		}
		tx_err = get_err
		return
	} )
	if err != nil { return }
	hub = &Store{ db: store , epoch: epoch , limits: limits }
	return
}

func ( hub *Store ) Epoch() ( epoch string ) {
	epoch = hub.epoch
	return
}

func seqKey( seq uint64 ) ( key []byte ) {
	key = make( []byte , 8 )
	binary.BigEndian.PutUint64( key , seq )
	return
}

// MaxSeq is the newest change number.
func ( hub *Store ) MaxSeq() ( seq uint64 ) {
	hub.db.View( func( tx *db.Tx ) ( tx_err error ) {
		if last := tx.LastKey( BucketFeed ); last != nil {
			seq = binary.BigEndian.Uint64( last )
		}
		return
	} )
	return
}

func ( hub *Store ) Info( now time.Time ) ( info syncproto.Info ) {
	info = syncproto.Info{
		Protocol:   syncproto.Protocol,
		HubEpoch:   hub.epoch,
		ServerTime: now.UnixMilli(),
		MaxSeq:     hub.MaxSeq(),
	}
	return
}

// Push stores every envelope that wins over what the hub has, in one
// transaction, and says what happened to each.
func ( hub *Store ) Push( envelopes []syncproto.Envelope , now time.Time ) ( results []syncproto.PushResult , err error ) {
	results = make( []syncproto.PushResult , len( envelopes ) )
	latest_allowed := now.Add( hub.limits.MaxFuture ).UnixMilli()
	err = hub.db.Write( func( tx *db.Tx ) ( tx_err error ) {
		for index , envelope := range envelopes {
			result := &results[ index ]
			result.Collection , result.ID = envelope.Collection , envelope.ID
			if envelope.Valid() == false || len( envelope.Body ) > hub.limits.MaxDocBytes {
				result.Status = syncproto.StatusRejectedInvalid
				continue
			}
			if envelope.HLC.Wall > latest_allowed {
				result.Status = syncproto.StatusRejectedFuture
				continue
			}
			key := []byte( envelope.Key() )
			var current stored
			get_err := tx.Get( BucketDocs , key , &current )
			switch {
			case get_err == nil:
				if syncproto.Wins( envelope , current.Envelope ) == false {
					// Already has this version or a newer one.
					result.Status , result.Seq = syncproto.StatusStale , current.Seq
					continue
				}
				if tx_err = tx.Delete( BucketFeed , seqKey( current.Seq ) ); tx_err != nil { return }
			case errors.Is( get_err , db.ErrNotFound ):
			default:
				tx_err = get_err
				return
			}
			seq , seq_err := tx.NextSequence( BucketFeed )
			if seq_err != nil {
				tx_err = seq_err
				return
			}
			if tx_err = tx.Put( BucketDocs , key , stored{ Envelope: envelope , Seq: seq } ); tx_err != nil { return }
			if tx_err = tx.Put( BucketFeed , seqKey( seq ) , envelope.Key() ); tx_err != nil { return }
			result.Status , result.Seq = syncproto.StatusOK , seq
		}
		return
	} )
	if err != nil {
		results = nil
	}
	return
}

// Pull returns the changes numbered after since, oldest first, at most limit
// of them.
func ( hub *Store ) Pull( since uint64 , limit int , now time.Time ) ( response syncproto.PullResponse , err error ) {
	if limit <= 0 || limit > hub.limits.PullPageMax {
		limit = hub.limits.PullPageMax
	}
	response = syncproto.PullResponse{
		Docs:       []syncproto.PulledDoc{},
		Next:       since,
		HubEpoch:   hub.epoch,
		ServerTime: now.UnixMilli(),
	}
	err = hub.db.View( func( tx *db.Tx ) ( tx_err error ) {
		if last := tx.LastKey( BucketFeed ); last != nil {
			response.MaxSeq = binary.BigEndian.Uint64( last )
		}
		tx_err = tx.ForEachFrom( BucketFeed , seqKey( since+1 ) , func() any { var key string; return &key } , func( raw_seq []byte , item any ) ( more bool ) {
			if len( response.Docs ) >= limit {
				response.HasMore = true
				return false
			}
			var current stored
			if tx.Get( BucketDocs , []byte( *item.( *string ) ) , &current ) != nil {
				return true
			}
			response.Docs = append( response.Docs , syncproto.PulledDoc{ Envelope: current.Envelope , Seq: binary.BigEndian.Uint64( raw_seq ) } )
			response.Next = binary.BigEndian.Uint64( raw_seq )
			return true
		} )
		return
	} )
	return
}

// Counts is how many records the hub holds, per collection, tombstones
// included.
func ( hub *Store ) Counts() ( counts map[string]int , err error ) {
	counts = map[string]int{}
	err = hub.db.View( func( tx *db.Tx ) ( tx_err error ) {
		tx_err = tx.ForEach( BucketDocs , func() any { return &stored{} } , func( _ []byte , item any ) bool {
			counts[ item.( *stored ).Envelope.Collection ]++
			return true
		} )
		return
	} )
	return
}
