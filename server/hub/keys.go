package hub

import (
	errors "errors"
	fmt "fmt"
	sort "sort"
	strings "strings"
	time "time"

	db "github.com/0187773933/MastersClosetTracker/server/db"
	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
)

// Station keys. Each station holds one, sent as Authorization: Bearer
// <id>.<secret>. The hub stores only a hash of the secret, so its database
// leaking hands out no working key. A key never expires on its own -- a
// station that stopped syncing because its key quietly timed out is worse
// than one an admin has to revoke deliberately -- and revoking one cuts that
// station off on its next request, without touching the others.

var ErrKeyNotFound = errors.New( "no such key" )

type Key struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	SecretHash string     `json:"secret_hash"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// KeyInfo is a key as listed: never the hash.
type KeyInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func ( key Key ) Info() ( info KeyInfo ) {
	info = KeyInfo{ ID: key.ID , Name: key.Name , CreatedAt: key.CreatedAt , RevokedAt: key.RevokedAt }
	return
}

// CreateKey makes a key and returns its credential -- the only time the
// secret exists outside the station that will hold it.
func ( hub *Store ) CreateKey( name string , now time.Time ) ( info KeyInfo , credential string , err error ) {
	name = strings.TrimSpace( name )
	if name == "" || len( name ) > 80 {
		err = fmt.Errorf( "a key needs a name (1-80 characters) saying which station holds it" )
		return
	}
	id := encryption.GenerateURLToken( 9 )
	secret := encryption.GenerateURLToken( 32 )
	key := Key{ ID: id , Name: name , SecretHash: encryption.Sha256Hex( secret ) , CreatedAt: now.UTC() }
	if err = hub.db.Put( BucketKeys , []byte( id ) , key ); err != nil { return }
	info , credential = key.Info() , id+"."+secret
	return
}

func ( hub *Store ) ListKeys() ( keys []KeyInfo , err error ) {
	keys = []KeyInfo{}
	err = hub.db.ForEach( BucketKeys , func() any { return &Key{} } , func( _ []byte , item any ) bool {
		keys = append( keys , item.( *Key ).Info() )
		return true
	} )
	sort.Slice( keys , func( i , j int ) bool { return keys[ i ].CreatedAt.Before( keys[ j ].CreatedAt ) } )
	return
}

func ( hub *Store ) RevokeKey( id string , now time.Time ) ( info KeyInfo , err error ) {
	err = hub.db.UpdateValue( BucketKeys , []byte( id ) , func() any { return &Key{} } , func( item any ) ( mutate_err error ) {
		key := item.( *Key )
		if key.RevokedAt == nil {
			revoked := now.UTC()
			key.RevokedAt = &revoked
		}
		info = key.Info()
		return
	} )
	if errors.Is( err , db.ErrNotFound ) {
		err = ErrKeyNotFound
	}
	return
}

func ( hub *Store ) DeleteKey( id string ) ( err error ) {
	var key Key
	if get_err := hub.db.Get( BucketKeys , []byte( id ) , &key ); get_err != nil {
		err = ErrKeyNotFound
		return
	}
	err = hub.db.Delete( BucketKeys , []byte( id ) )
	return
}

// Authenticate checks a credential and returns its live key.
func ( hub *Store ) Authenticate( credential string ) ( key Key , ok bool ) {
	id , secret , found := strings.Cut( strings.TrimSpace( credential ) , "." )
	if found == false || id == "" || secret == "" || len( credential ) > 200 { return }
	if hub.db.Get( BucketKeys , []byte( id ) , &key ) != nil { return }
	if key.RevokedAt != nil { return }
	ok = encryption.ConstantTimeEqual( key.SecretHash , encryption.Sha256Hex( secret ) )
	return
}
