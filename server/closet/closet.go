// Package closet is what the closet does, as operations: register a family,
// edit them, check them in, look them up. Routes call these and nothing
// lower, so every rule -- normalising names, capping families, minting
// barcodes, the double-check-in guard -- lives in one place whichever page,
// API key or import triggers it.
//
// It writes through the synced document store and reads through the
// in-memory index, so a change made here is on its way to the other stations
// before the call returns.
package closet

import (
	errors "errors"
	fmt "fmt"
	strings "strings"
	sync "sync"
	time "time"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	index "github.com/0187773933/MastersClosetTracker/server/index"
	settings "github.com/0187773933/MastersClosetTracker/server/settings"
	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

var (
	ErrNotFound = errors.New( "not found" )

	// ErrStale is an edit made from a page that loaded the record before
	// someone else (here or at another station) changed it.
	ErrStale = errors.New( "this record was changed elsewhere since you opened it -- reload and try again" )

	// ErrRecentVisit is a second check-in for the same family within a
	// couple of minutes: a double click, or two volunteers at once.
	ErrRecentVisit = errors.New( "this family was just checked in -- check the history before doing it again" )

	ErrInvalid = errors.New( "invalid request" )
)

// Buckets is every bucket the closet's data lives in, for db.Open -- the
// server and the manage CLI both open the database with it.
var Buckets = append( append( []string{} , docs.Buckets... ) , settings.Buckets... )

// RecentVisitWindow is how close together two check-ins for one family must
// be to be refused as a duplicate.
const RecentVisitWindow = 2 * time.Minute

type Service struct {
	Docs     *docs.Store
	Index    *index.Index
	Settings *settings.Service

	// Station names the visits this station records.
	Station string

	visit_mu    sync.Mutex
	last_visits map[string]time.Time
}

func New( docs_store *docs.Store , idx *index.Index , settings_service *settings.Service , station string ) ( service *Service ) {
	service = &Service{
		Docs:        docs_store,
		Index:       idx,
		Settings:    settings_service,
		Station:     station,
		last_visits: map[string]time.Time{},
	}
	return
}

// Now is the current time in the hub's timeline (see hlc.Clock.Corrected),
// so a laptop with a wrong clock still files visits under the right day.
func ( service *Service ) Now() ( now time.Time ) {
	now = service.Docs.Clock().Corrected()
	return
}

func ( service *Service ) Location() ( location *time.Location ) {
	location = service.Settings.Org().Location
	return
}

// ---------------------------------------------------------------------------
// clients
// ---------------------------------------------------------------------------

func ( service *Service ) barcodeTaken( barcode string ) ( taken bool ) {
	taken = service.Index.BarcodeTaken( barcode )
	return
}

// prepare applies every save-time rule to a profile.
func ( service *Service ) prepare( client *clients.Client , existing_size int ) ( err error ) {
	org := service.Settings.Org()
	clients.Normalize( client )
	clients.ClampFamily( client , org.MaxFamilySize , existing_size )
	clients.AutoVerify( client , org.AutoVerifyZipcode )
	if len( client.Barcodes ) == 0 {
		var barcode string
		if barcode , err = clients.MintBarcode( service.barcodeTaken ); err != nil { return }
		client.Barcodes = []string{ barcode }
	}
	return
}

// CreateClient registers a family. With no name at all it becomes a
// numbered "Temp" record, so a volunteer can start a check-in and fill the
// name in later.
func ( service *Service ) CreateClient( input clients.Client ) ( created index.Client , err error ) {
	client := input
	identity := &client.Identity
	if strings.TrimSpace( identity.FirstName+identity.MiddleName+identity.LastName ) == "" {
		identity.FirstName = "Temp"
		identity.MiddleName = fmt.Sprintf( "%06d" , encryption.RandomUint64()%1_000_000 )
		identity.LastName = fmt.Sprintf( "%06d" , encryption.RandomUint64()%1_000_000 )
	}
	client.CreatedAt = service.Now().UTC()
	if err = service.prepare( &client , 0 ); err != nil { return }
	id := encryption.NewUUID()
	if _ , err = service.Docs.Put( clients.Collection , id , client ); err != nil { return }
	created , _ = service.Index.Client( id )
	return
}

// UpdateClient replaces a family's profile with what an edit page sent.
// expect is the version the page loaded (its _rev); an edit based on an
// older version is refused rather than silently undoing someone else's.
// The field a page never edits (when they registered) is carried over.
func ( service *Service ) UpdateClient( id string , input clients.Client , expect hlc.Stamp ) ( updated index.Client , err error ) {
	existing , ok := service.Index.Client( id )
	if ok == false {
		err = ErrNotFound
		return
	}
	if expect.IsZero() == false && expect != existing.HLC {
		err = ErrStale
		return
	}
	client := input
	client.CreatedAt = existing.Record.CreatedAt
	if err = service.prepare( &client , existing.Record.FamilySize ); err != nil { return }
	if _ , err = service.Docs.PutIf( clients.Collection , id , client , existing.HLC ); err != nil {
		if errors.Is( err , docs.ErrConflict ) {
			err = ErrStale
		}
		return
	}
	updated , _ = service.Index.Client( id )
	return
}

// DeleteClient removes a family. Their visits stay stored but are no longer
// counted or shown anywhere.
func ( service *Service ) DeleteClient( id string ) ( err error ) {
	if _ , ok := service.Index.Client( id ); ok == false {
		err = ErrNotFound
		return
	}
	_ , err = service.Docs.Delete( clients.Collection , id )
	return
}

// Similar is every other family that looks like the same one.
func ( service *Service ) Similar( id string ) ( matches []index.Client , err error ) {
	target , ok := service.Index.Client( id )
	if ok == false {
		err = ErrNotFound
		return
	}
	threshold := service.Settings.Org().SimilarityThreshold
	for _ , other := range service.Index.Clients() {
		if other.ID == id { continue }
		if clients.Compare( &target.Record , &other.Record , threshold ).Any() {
			matches = append( matches , other )
		}
	}
	return
}

// ---------------------------------------------------------------------------
// visits
// ---------------------------------------------------------------------------

// Standing is everything the check-in screen needs about a family right now.
type Standing struct {
	Client  index.Client
	Outcome visits.Outcome
	Balance visits.Balance
	History []visits.Entry
}

func ( service *Service ) Standing( id string ) ( standing Standing , err error ) {
	client , ok := service.Index.Client( id )
	if ok == false {
		err = ErrNotFound
		return
	}
	org := service.Settings.Org()
	now := service.Now()
	history := service.Index.VisitsOf( id )
	standing = Standing{
		Client:  client,
		History: history,
		Outcome: visits.Evaluate( history , now , org.CooloffDays , org.Location ),
		Balance: visits.ComputeBalance( history , client.Record.FamilySize , org.Balance , now , org.CooloffDays , org.Location ),
	}
	return
}

// RecordShopping records a staff check-in from the ticket the check-in page
// submitted. It does not consult the cooloff: the page shows the cooloff,
// and a volunteer may still serve a family (that is a judgement call, not
// the server's). It does refuse an exact repeat within a couple of minutes.
func ( service *Service ) RecordShopping( id string , request visits.ShoppingRequest ) ( entry visits.Entry , client index.Client , err error ) {
	client , ok := service.Index.Client( id )
	if ok == false {
		err = ErrNotFound
		return
	}
	now := service.Now()

	// request.Again is the volunteer saying "yes, check them in again".
	service.visit_mu.Lock()
	if last , seen := service.last_visits[ id ]; seen && now.Sub( last ) < RecentVisitWindow && request.Again == false {
		service.visit_mu.Unlock()
		err = ErrRecentVisit
		return
	}
	service.last_visits[ id ] = now
	service.visit_mu.Unlock()

	// A visit that synced in from another station counts too.
	if last , found := visits.LastVisit( service.Index.VisitsOf( id ) ); found && now.Sub( last.Visit.At ) < RecentVisitWindow && now.Sub( last.Visit.At ) >= 0 && request.Again == false {
		err = ErrRecentVisit
		return
	}

	org := service.Settings.Org()
	cleaned := request.Clean( org.Balance.General.Total )
	barcode := ""
	if len( client.Record.Barcodes ) > 0 {
		barcode = client.Record.Barcodes[ 0 ]
	}
	if cleaned.FamilyName == "" {
		cleaned.FamilyName = client.Record.Name()
	}
	if cleaned.Language == "" {
		cleaned.Language = client.Record.Language
	}
	visit := visits.NewShoppingVisit( id , client.Record.Name() , cleaned , barcode , now , org.Location , service.Station )
	visit_id := NewVisitID( now )
	if _ , err = service.Docs.Put( visits.Collection , visit_id , visit ); err != nil {
		service.visit_mu.Lock()
		delete( service.last_visits , id )
		service.visit_mu.Unlock()
		return
	}
	entry = visits.Entry{ ID: visit_id , Visit: visit }
	return
}

// DeleteVisit removes one visit (a duplicate, a mistake).
func ( service *Service ) DeleteVisit( id string ) ( err error ) {
	if _ , ok := service.Index.Visit( id ); ok == false {
		err = ErrNotFound
		return
	}
	_ , err = service.Docs.Delete( visits.Collection , id )
	return
}

// SetTally records the paper-ticket count for a day. A nil field keeps what
// was there, so saving just the count never wipes the note.
func ( service *Service ) SetTally( day string , shopped_for *int , note *string ) ( tally visits.Tally , err error ) {
	if _ , parse_err := time.Parse( visits.DayLayout , day ); parse_err != nil {
		err = ErrInvalid
		return
	}
	tally = service.Index.Tally( day )
	if shopped_for != nil {
		tally.ShoppedFor = min( max( *shopped_for , 0 ) , 100000 )
	}
	if note != nil {
		tally.Note = strings.TrimSpace( *note )
		if len( tally.Note ) > 2000 {
			tally.Note = tally.Note[ :2000 ]
		}
	}
	_ , err = service.Docs.Put( visits.TallyCollection , day , tally )
	return
}

// NewVisitID is a ULID-shaped id: the time it was made, then randomness.
// Ids made on different stations never collide, and sort roughly by time.
func NewVisitID( at time.Time ) ( id string ) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	millis := uint64( at.UnixMilli() )
	var out [ 26 ]byte
	for position := 9; position >= 0; position-- {
		out[ position ] = alphabet[ millis%32 ]
		millis /= 32
	}
	random := encryption.GenerateRandomBytes( 16 )
	for position := 10; position < 26; position++ {
		out[ position ] = alphabet[ random[ position-10 ]%32 ]
	}
	id = string( out[ : ] )
	return
}
