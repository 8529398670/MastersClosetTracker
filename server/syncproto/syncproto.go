// Package syncproto is the wire format between stations and the hub, and the
// one conflict rule both sides apply. Keeping the rule here -- not once in the
// hub and once in the station -- is what guarantees every machine settles the
// same conflict the same way.
package syncproto

import (
	bytes "bytes"
	json "encoding/json"
	regexp "regexp"

	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
)

// Protocol is bumped on any incompatible change to the messages below.
const Protocol = 1

// Envelope is one record as it travels and as the hub stores it. Body is the
// record's own JSON; the hub never looks inside it. A deleted record keeps
// its envelope (a tombstone) with no body, so the deletion itself syncs.
type Envelope struct {
	Collection string          `json:"c"`
	ID         string          `json:"id"`
	HLC        hlc.Stamp       `json:"hlc"`
	Node       string          `json:"node"`
	Deleted    bool            `json:"del,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// Key identifies the record across machines.
func ( envelope Envelope ) Key() ( key string ) {
	key = envelope.Collection + "\x00" + envelope.ID
	return
}

// hash breaks the last possible tie: two different versions with the same
// stamp and node, which only a restored or cloned database could produce.
func ( envelope Envelope ) hash() ( sum [32]byte ) {
	marker := []byte{ 0 }
	body := []byte( envelope.Body )
	if envelope.Deleted {
		marker , body = []byte{ 1 } , nil
	}
	copy( sum[ : ] , encryption.Sha256Sum( [][]byte{ marker , body } ) )
	return
}

// Wins reports whether a should replace b. Later stamp wins; on an equal
// stamp the higher node id wins; on an equal node the larger body hash wins.
// It is a total order, so applying the same set of versions in any order on
// any machine ends on the same one.
func Wins( a Envelope , b Envelope ) ( result bool ) {
	if compared := a.HLC.Compare( b.HLC ); compared != 0 {
		result = compared > 0
		return
	}
	if a.Node != b.Node {
		result = a.Node > b.Node
		return
	}
	a_hash , b_hash := a.hash() , b.hash()
	result = bytes.Compare( a_hash[ : ] , b_hash[ : ] ) > 0
	return
}

// Same reports whether two envelopes are the same version.
func Same( a Envelope , b Envelope ) ( result bool ) {
	result = a.HLC == b.HLC && a.Node == b.Node && a.hash() == b.hash()
	return
}

var (
	collectionPattern = regexp.MustCompile( `^[a-z][a-z0-9_]{0,31}$` )
	idPattern         = regexp.MustCompile( `^[A-Za-z0-9._:-]{1,128}$` )
	nodePattern       = regexp.MustCompile( `^[A-Za-z0-9_-]{1,64}$` )
)

// Valid checks the parts of an envelope the hub relies on. The body is only
// checked for being JSON (and absent on a tombstone).
func ( envelope Envelope ) Valid() ( ok bool ) {
	if collectionPattern.MatchString( envelope.Collection ) == false { return }
	if idPattern.MatchString( envelope.ID ) == false { return }
	if nodePattern.MatchString( envelope.Node ) == false { return }
	if envelope.HLC.IsZero() { return }
	if envelope.Deleted {
		ok = len( envelope.Body ) == 0
		return
	}
	ok = len( envelope.Body ) > 0 && json.Valid( envelope.Body )
	return
}

// ValidCollection and ValidID are the same rules, for code that builds keys.
func ValidCollection( name string ) ( ok bool ) {
	ok = collectionPattern.MatchString( name )
	return
}

func ValidID( id string ) ( ok bool ) {
	ok = idPattern.MatchString( id )
	return
}

// ---------------------------------------------------------------------------
// messages
// ---------------------------------------------------------------------------

// Info is GET /sync/v1/info. HubEpoch is random per hub database: when a
// station sees it change, the hub was wiped or replaced, and the station
// starts its cursor over and re-sends everything it has.
type Info struct {
	Protocol   int    `json:"protocol"`
	HubEpoch   string `json:"hub_epoch"`
	ServerTime int64  `json:"server_time"` // unix milliseconds
	MaxSeq     uint64 `json:"max_seq"`
}

// PushRequest is POST /sync/v1/push.
type PushRequest struct {
	Docs []Envelope `json:"docs"`
}

// Push result statuses. ok and stale both mean "the hub has this version or
// a newer one" -- the station can stop sending it. rejected_future means the
// stamp is too far ahead of the hub's clock; the station re-stamps it.
// rejected_invalid is a malformed envelope and will never be accepted.
const (
	StatusOK              = "ok"
	StatusStale           = "stale"
	StatusRejectedFuture  = "rejected_future"
	StatusRejectedInvalid = "rejected_invalid"
)

type PushResult struct {
	Collection string `json:"c"`
	ID         string `json:"id"`
	Status     string `json:"status"`
	Seq        uint64 `json:"seq,omitempty"`
}

type PushResponse struct {
	Results    []PushResult `json:"results"`
	HubEpoch   string       `json:"hub_epoch"`
	ServerTime int64        `json:"server_time"`
}

// PulledDoc is an envelope with the hub's sequence number for it.
type PulledDoc struct {
	Envelope
	Seq uint64 `json:"seq"`
}

// PullResponse is GET /sync/v1/pull?since=N&limit=M. Docs are in sequence
// order; Next is the cursor to send next time; HasMore says to ask again
// right away rather than waiting for the poll interval.
type PullResponse struct {
	Docs       []PulledDoc `json:"docs"`
	Next       uint64      `json:"next"`
	HasMore    bool        `json:"has_more"`
	MaxSeq     uint64      `json:"max_seq"`
	HubEpoch   string      `json:"hub_epoch"`
	ServerTime int64       `json:"server_time"`
}

// ErrorResponse is the body of any non-2xx answer.
type ErrorResponse struct {
	Error string `json:"error"`
}
