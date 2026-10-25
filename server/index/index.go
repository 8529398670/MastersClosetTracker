// Package index keeps every client, visit and day tally in memory, in the
// shapes the pages ask for: by id, by barcode, by name, by family, by day.
//
// It is built from the document store at startup and kept current by the
// store's hooks, which fire for local writes and synced ones alike -- so
// there are no index buckets on disk to drift out of step with the records,
// and a lookup never has to decrypt the whole database. A closet's data (a
// few thousand families, tens of thousands of visits) fits comfortably.
//
// Deleted clients disappear from every answer, and so do their visits: a
// delete is one tombstone, never a cascade, and a visit that synced in
// before its family's record (or after its deletion) is simply not shown.
package index

import (
	sort "sort"
	strings "strings"
	sync "sync"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

// Client is one client as the index holds it.
type Client struct {
	ID      string
	HLC     hlc.Stamp
	Deleted bool
	Record  clients.Client
	tokens  []string
}

type Index struct {
	mu sync.RWMutex

	clients  map[string]*Client
	barcodes map[string]map[string]bool // every barcode ever seen, deleted clients included
	visits   map[string]visits.Entry
	byClient map[string]map[string]bool
	byDay    map[string]map[string]bool
	tallies  map[string]visits.Tally
}

func New() ( index *Index ) {
	index = &Index{
		clients:  map[string]*Client{},
		barcodes: map[string]map[string]bool{},
		visits:   map[string]visits.Entry{},
		byClient: map[string]map[string]bool{},
		byDay:    map[string]map[string]bool{},
		tallies:  map[string]visits.Tally{},
	}
	return
}

// Attach loads everything from store and follows its changes from then on.
func ( index *Index ) Attach( store *docs.Store ) ( err error ) {
	for collection , apply := range map[string]func( docs.Envelope ){
		clients.Collection:      index.putClient,
		visits.Collection:       index.putVisit,
		visits.TallyCollection:  index.putTally,
	} {
		apply := apply
		if err = store.ForEach( collection , func( envelope docs.Envelope ) bool {
			apply( envelope )
			return true
		} ); err != nil { return }
		store.Hook( collection , func( current docs.Envelope , _ *docs.Envelope ) {
			apply( current )
		} )
	}
	return
}

func addTo( sets map[string]map[string]bool , key string , id string ) {
	set := sets[ key ]
	if set == nil {
		set = map[string]bool{}
		sets[ key ] = set
	}
	set[ id ] = true
}

func removeFrom( sets map[string]map[string]bool , key string , id string ) {
	if set := sets[ key ]; set != nil {
		delete( set , id )
		if len( set ) == 0 {
			delete( sets , key )
		}
	}
}

func ( index *Index ) putClient( envelope docs.Envelope ) {
	entry := &Client{ ID: envelope.ID , HLC: envelope.HLC , Deleted: envelope.Deleted }
	if envelope.Deleted == false {
		if docs.Decode( envelope , &entry.Record ) != nil {
			return
		}
		entry.tokens = entry.Record.SearchTokens()
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if previous := index.clients[ envelope.ID ]; previous != nil && envelope.Deleted {
		// Keep the last known profile on the tombstone so its barcodes stay
		// reserved; it is never returned.
		entry.Record = previous.Record
	}
	index.clients[ envelope.ID ] = entry
	for _ , barcode := range entry.Record.Barcodes {
		addTo( index.barcodes , barcode , envelope.ID )
	}
}

func ( index *Index ) putVisit( envelope docs.Envelope ) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if previous , ok := index.visits[ envelope.ID ]; ok {
		removeFrom( index.byClient , previous.Visit.ClientID , envelope.ID )
		removeFrom( index.byDay , previous.Visit.Day , envelope.ID )
		delete( index.visits , envelope.ID )
	}
	if envelope.Deleted { return }
	var visit visits.Visit
	if docs.Decode( envelope , &visit ) != nil { return }
	index.visits[ envelope.ID ] = visits.Entry{ ID: envelope.ID , Visit: visit }
	addTo( index.byClient , visit.ClientID , envelope.ID )
	addTo( index.byDay , visit.Day , envelope.ID )
}

func ( index *Index ) putTally( envelope docs.Envelope ) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if envelope.Deleted {
		delete( index.tallies , envelope.ID )
		return
	}
	var tally visits.Tally
	if docs.Decode( envelope , &tally ) == nil {
		index.tallies[ envelope.ID ] = tally
	}
}

// live reports whether a client exists and is not deleted. Callers hold mu.
func ( index *Index ) live( id string ) ( entry *Client , ok bool ) {
	entry = index.clients[ id ]
	ok = entry != nil && entry.Deleted == false
	return
}

// ---------------------------------------------------------------------------
// clients
// ---------------------------------------------------------------------------

// Client returns a live client.
func ( index *Index ) Client( id string ) ( client Client , ok bool ) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	entry , ok := index.live( id )
	if ok {
		client = *entry
	}
	return
}

// Clients is every live client, in no particular order.
func ( index *Index ) Clients() ( result []Client ) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	for _ , entry := range index.clients {
		if entry.Deleted == false {
			result = append( result , *entry )
		}
	}
	return
}

// ByBarcode is every live client carrying barcode -- normally one, but two
// families can end up sharing a card number, and the page should see both
// rather than silently get the first.
func ( index *Index ) ByBarcode( barcode string ) ( result []Client ) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	for id := range index.barcodes[ barcode ] {
		entry , ok := index.live( id )
		if ok == false { continue }
		for _ , candidate := range entry.Record.Barcodes {
			if candidate == barcode {
				result = append( result , *entry )
				break
			}
		}
	}
	sort.Slice( result , func( i , j int ) bool { return result[ i ].ID < result[ j ].ID } )
	return
}

// BarcodeTaken reports whether any client, deleted or not, has ever had
// barcode. Minting avoids all of them.
func ( index *Index ) BarcodeTaken( barcode string ) ( taken bool ) {
	index.mu.RLock()
	taken = len( index.barcodes[ barcode ] ) > 0
	index.mu.RUnlock()
	return
}

// wordMatches is the type-ahead rule: a typed word matches a name word that
// starts with it ("pear" finds Pearson while typing) or is one typo away
// from it ("pearsen").
func wordMatches( typed string , token string ) ( score int ) {
	switch {
	case typed == token:
		score = 3
	case strings.HasPrefix( token , typed ):
		score = 2
	case len( []rune( typed ) ) >= 3 && clients.Levenshtein( typed , token ) <= 1:
		score = 1
	}
	return
}

// Search finds live clients whose names match every typed word, best
// matches first.
func ( index *Index ) Search( query string , limit int ) ( result []Client ) {
	words := strings.Fields( clients.Fold( query ) )
	if len( words ) == 0 { return }
	type hit struct {
		client Client
		score  int
	}
	var hits []hit
	index.mu.RLock()
	for _ , entry := range index.clients {
		if entry.Deleted { continue }
		total := 0
		for _ , word := range words {
			best := 0
			for _ , token := range entry.tokens {
				best = max( best , wordMatches( word , token ) )
			}
			if best == 0 {
				total = 0
				break
			}
			total += best
		}
		if total > 0 {
			hits = append( hits , hit{ *entry , total } )
		}
	}
	index.mu.RUnlock()
	sort.Slice( hits , func( i , j int ) bool {
		if hits[ i ].score != hits[ j ].score {
			return hits[ i ].score > hits[ j ].score
		}
		name_i , name_j := hits[ i ].client.Record.Name() , hits[ j ].client.Record.Name()
		if name_i != name_j {
			return name_i < name_j
		}
		return hits[ i ].client.ID < hits[ j ].client.ID
	} )
	for _ , item := range hits {
		if limit > 0 && len( result ) >= limit { break }
		result = append( result , item.client )
	}
	return
}

// ---------------------------------------------------------------------------
// visits
// ---------------------------------------------------------------------------

// VisitsOf is a live client's whole history, refused attempts included,
// oldest first.
func ( index *Index ) VisitsOf( client_id string ) ( result []visits.Entry ) {
	index.mu.RLock()
	if _ , ok := index.live( client_id ); ok {
		for id := range index.byClient[ client_id ] {
			result = append( result , index.visits[ id ] )
		}
	}
	index.mu.RUnlock()
	visits.SortByTime( result )
	return
}

// FirstVisits maps each of client_ids to the id of its earliest visit that
// counts -- the visit that was the family's first time in.
func ( index *Index ) FirstVisits( client_ids []string ) ( result map[string]string ) {
	result = map[string]string{}
	index.mu.RLock()
	defer index.mu.RUnlock()
	for _ , client_id := range client_ids {
		var first visits.Entry
		found := false
		for id := range index.byClient[ client_id ] {
			entry := index.visits[ id ]
			if entry.Visit.Counts() == false { continue }
			if found == false || entry.Visit.At.Before( first.Visit.At ) || ( entry.Visit.At.Equal( first.Visit.At ) && entry.ID < first.ID ) {
				first , found = entry , true
			}
		}
		if found {
			result[ client_id ] = first.ID
		}
	}
	return
}

// Visit returns one visit, if its family is live.
func ( index *Index ) Visit( id string ) ( entry visits.Entry , ok bool ) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	entry , ok = index.visits[ id ]
	if ok {
		_ , ok = index.live( entry.Visit.ClientID )
	}
	return
}

// Day is every visit that counts on one day, oldest first.
func ( index *Index ) Day( day string ) ( result []visits.Entry ) {
	index.mu.RLock()
	for id := range index.byDay[ day ] {
		entry := index.visits[ id ]
		if entry.Visit.Counts() == false { continue }
		if _ , ok := index.live( entry.Visit.ClientID ); ok {
			result = append( result , entry )
		}
	}
	index.mu.RUnlock()
	visits.SortByTime( result )
	return
}

// Days is one summary per collection day, newest first.
func ( index *Index ) Days() ( result []visits.DaySummary ) {
	index.mu.RLock()
	var entries []visits.Entry
	for _ , entry := range index.visits {
		if _ , ok := index.live( entry.Visit.ClientID ); ok {
			entries = append( entries , entry )
		}
	}
	tallies := make( map[string]visits.Tally , len( index.tallies ) )
	for day , tally := range index.tallies {
		tallies[ day ] = tally
	}
	index.mu.RUnlock()
	result = visits.Summarize( entries , tallies )
	return
}

// Tally is one day's hand-written count (zero if none).
func ( index *Index ) Tally( day string ) ( tally visits.Tally ) {
	index.mu.RLock()
	tally = index.tallies[ day ]
	index.mu.RUnlock()
	return
}

// Recent is one row per family that has visited, most recent visit first.
type RecentRow struct {
	Client Client
	Last   visits.Entry
	Count  int
}

func ( index *Index ) Recent( limit int ) ( result []RecentRow ) {
	index.mu.RLock()
	for client_id , ids := range index.byClient {
		entry , ok := index.live( client_id )
		if ok == false { continue }
		history := make( []visits.Entry , 0 , len( ids ) )
		for id := range ids {
			history = append( history , index.visits[ id ] )
		}
		last , found := visits.LastVisit( history )
		if found == false { continue }
		count := 0
		for _ , item := range history {
			if item.Visit.Counts() {
				count++
			}
		}
		result = append( result , RecentRow{ Client: *entry , Last: last , Count: count } )
	}
	index.mu.RUnlock()
	sort.Slice( result , func( i , j int ) bool {
		if result[ i ].Last.Visit.At.Equal( result[ j ].Last.Visit.At ) {
			return result[ i ].Client.ID < result[ j ].Client.ID
		}
		return result[ i ].Last.Visit.At.After( result[ j ].Last.Visit.At )
	} )
	if limit > 0 && len( result ) > limit {
		result = result[ :limit ]
	}
	return
}

// Counts is how many live clients and counted visits there are.
func ( index *Index ) Counts() ( live_clients int , counted_visits int ) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	for _ , entry := range index.clients {
		if entry.Deleted == false {
			live_clients++
		}
	}
	for _ , entry := range index.visits {
		if _ , ok := index.live( entry.Visit.ClientID ); ok && entry.Visit.Counts() {
			counted_visits++
		}
	}
	return
}
