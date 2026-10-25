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
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	sort "sort"
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

// prepare applies every save-time rule to a profile. previous is the
// record as it was before this save (nil for a new one).
func ( service *Service ) prepare( client *clients.Client , previous *clients.Client ) ( err error ) {
	existing_size := 0
	if previous != nil {
		existing_size = previous.FamilySize
	}
	org := service.Settings.Org()
	clients.Normalize( client )
	clients.ClampFamily( client , org.MaxFamilySize , existing_size )
	clients.AutoVerify( client , org.AutoVerifyZipcode )
	if len( client.Barcodes ) == 0 {
		var barcode string
		if barcode , err = clients.MintBarcode( service.barcodeTaken ); err != nil { return }
		client.Barcodes = []string{ barcode }
	}
	clients.StampBarcodes( client , previous , service.Now().UTC() )
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
	if err = service.prepare( &client , nil ); err != nil { return }
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
	if err = service.prepare( &client , &existing.Record ); err != nil { return }
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

// DuplicateGroup is records that look like the same person registered
// more than once.
type DuplicateGroup struct {
	Members []DuplicateMember
}

// DuplicateMember is one record in a group and what it has in common with
// another member ("name", "phone", ...).
type DuplicateMember struct {
	Client  index.Client
	Reasons []string
}

// duplicateFields are the details compared within a group, in the order
// they are reported. Only the person-level ones (personal) put records in
// the same group: a household shares a phone, an email and an address, so
// those alone would join a mother and daughter, and through them anyone
// else at that address. A barcode is not one either -- two families end up
// on one card number when cards are typed in by hand or handed out twice
// (the barcodes page flags those) -- so it joins two records only when
// their names are alike too, which catches a misspelled re-registration.
var duplicateFields = []struct {
	name     string
	personal bool
	keys     func( record *clients.Client ) []string
}{
	{ "name" , true , func( record *clients.Client ) []string {
		first , last := clients.Fold( strings.TrimSpace( record.Identity.FirstName ) ) , clients.Fold( strings.TrimSpace( record.Identity.LastName ) )
		if first == "" || last == "" { return nil }
		return []string{ first + "|" + last }
	} },
	{ "birthday" , true , func( record *clients.Client ) []string {
		birthday := record.Identity.DateOfBirth
		first := clients.Fold( strings.TrimSpace( record.Identity.FirstName ) )
		if first == "" || birthday.Year == 0 || birthday.Month == "" || birthday.Day == 0 { return nil }
		return []string{ fmt.Sprintf( "%s|%d|%s|%d" , first , birthday.Year , clients.Fold( birthday.Month ) , birthday.Day ) }
	} },
	{ "barcode" , false , func( record *clients.Client ) []string { return record.Barcodes } },
	{ "phone" , false , func( record *clients.Client ) []string {
		if phone := strings.TrimSpace( record.PhoneNumber ); phone != "" { return []string{ phone } }
		return nil
	} },
	{ "email" , false , func( record *clients.Client ) []string {
		if email := clients.Fold( strings.TrimSpace( record.EmailAddress ) ); email != "" { return []string{ email } }
		return nil
	} },
	{ "address" , false , func( record *clients.Client ) []string {
		address := record.Identity.Address
		number , street := clients.Fold( strings.TrimSpace( address.StreetNumber ) ) , clients.Fold( strings.Join( strings.Fields( address.StreetName ) , " " ) )
		if number == "" || street == "" { return nil }
		return []string{ number + "|" + street }
	} },
}

// Duplicates is every group of records that look like the same person:
// the same first and last name, the same first name and birthday, or a
// shared barcode with a near-identical name. Largest groups first.
func ( service *Service ) Duplicates() ( groups []DuplicateGroup ) {
	groups = []DuplicateGroup{}
	all := service.Index.Clients()
	parent := make( []int , len( all ) )
	for position := range parent {
		parent[ position ] = position
	}
	root := func( position int ) int {
		for parent[ position ] != position {
			parent[ position ] = parent[ parent[ position ] ]
			position = parent[ position ]
		}
		return position
	}
	keys := make( [][][]string , len( all ) ) // [record][field] -> keys
	for position := range all {
		keys[ position ] = make( [][]string , len( duplicateFields ) )
		for field , spec := range duplicateFields {
			keys[ position ][ field ] = spec.keys( &all[ position ].Record )
		}
	}
	for field , spec := range duplicateFields {
		if spec.personal == false { continue }
		first := map[string]int{}
		for position := range all {
			for _ , key := range keys[ position ][ field ] {
				if key == "" { continue }
				if seen , ok := first[ key ]; ok {
					parent[ root( position ) ] = root( seen )
				} else {
					first[ key ] = position
				}
			}
		}
	}
	threshold := service.Settings.Org().SimilarityThreshold
	holders := map[string][]int{}
	for position := range all {
		for _ , barcode := range all[ position ].Record.Barcodes {
			if barcode != "" {
				holders[ barcode ] = append( holders[ barcode ] , position )
			}
		}
	}
	for _ , positions := range holders {
		for a := range positions {
			for b := a + 1; b < len( positions ); b++ {
				x , y := positions[ a ] , positions[ b ]
				if clients.Compare( &all[ x ].Record , &all[ y ].Record , threshold ).Name {
					parent[ root( x ) ] = root( y )
				}
			}
		}
	}
	members := map[int][]int{}
	for position := range all {
		members[ root( position ) ] = append( members[ root( position ) ] , position )
	}
	for _ , positions := range members {
		if len( positions ) < 2 { continue }
		// A member's reasons are the details it shares with another member.
		count := make( []map[string]int , len( duplicateFields ) )
		for field := range duplicateFields {
			count[ field ] = map[string]int{}
			for _ , position := range positions {
				for _ , key := range keys[ position ][ field ] {
					count[ field ][ key ]++
				}
			}
		}
		group := DuplicateGroup{}
		for _ , position := range positions {
			member := DuplicateMember{ Client: all[ position ] }
			for field , spec := range duplicateFields {
				for _ , key := range keys[ position ][ field ] {
					if key != "" && count[ field ][ key ] > 1 {
						member.Reasons = append( member.Reasons , spec.name )
						break
					}
				}
			}
			group.Members = append( group.Members , member )
		}
		sort.Slice( group.Members , func( a int , b int ) bool {
			return group.Members[ a ].Client.Record.Name() < group.Members[ b ].Client.Record.Name()
		} )
		groups = append( groups , group )
	}
	sort.Slice( groups , func( a int , b int ) bool {
		if len( groups[ a ].Members ) != len( groups[ b ].Members ) {
			return len( groups[ a ].Members ) > len( groups[ b ].Members )
		}
		return groups[ a ].Members[ 0 ].Client.Record.Name() < groups[ b ].Members[ 0 ].Client.Record.Name()
	} )
	return
}

// MergeClients folds others into keep: one family left, with every
// barcode and every visit. keep's own details win; a detail keep is
// missing is taken from the others, and keep's family members stay unless
// it lists none. expect is the version of keep the page saw (zero to
// skip the check). Visits move first and the others are deleted last, so
// a failure part way never strands a visit on a deleted family.
func ( service *Service ) MergeClients( keep string , others []string , expect hlc.Stamp ) ( merged index.Client , err error ) {
	kept , ok := service.Index.Client( keep )
	if ok == false {
		err = ErrNotFound
		return
	}
	if expect.IsZero() == false && expect != kept.HLC {
		err = ErrStale
		return
	}
	seen := map[string]bool{ keep: true }
	var sources []index.Client
	for _ , id := range others {
		if seen[ id ] {
			err = ErrInvalid
			return
		}
		seen[ id ] = true
		other , found := service.Index.Client( id )
		if found == false {
			err = ErrNotFound
			return
		}
		sources = append( sources , other )
	}
	if len( sources ) == 0 {
		err = ErrInvalid
		return
	}

	record := kept.Record
	record.Barcodes = append( []string{} , kept.Record.Barcodes... )
	record.AuthorizedAliases = append( []clients.Person{} , kept.Record.AuthorizedAliases... )
	assigned := map[string]time.Time{}
	for barcode , at := range kept.Record.BarcodeAssigned {
		assigned[ barcode ] = at
	}
	extra := map[string]json.RawMessage{}
	for name , value := range kept.Record.Extra {
		extra[ name ] = value
	}
	aliases := map[string]bool{}
	alias_key := func( person clients.Person ) string {
		return clients.Fold( person.FirstName + " " + person.LastName )
	}
	for _ , person := range record.AuthorizedAliases {
		aliases[ alias_key( person ) ] = true
	}
	fill := func( into *string , from string ) {
		if strings.TrimSpace( *into ) == "" {
			*into = from
		}
	}
	largest := kept.Record.FamilySize
	for _ , source := range sources {
		other := source.Record
		record.Barcodes = append( record.Barcodes , other.Barcodes... )
		for barcode , at := range other.BarcodeAssigned {
			if current , has := assigned[ barcode ]; has == false || at.Before( current ) {
				assigned[ barcode ] = at
			}
		}
		fill( &record.EmailAddress , other.EmailAddress )
		fill( &record.PhoneNumber , other.PhoneNumber )
		fill( &record.Language , other.Language )
		fill( &record.Identity.MiddleName , other.Identity.MiddleName )
		if record.Identity.Address == ( clients.Address{} ) {
			record.Identity.Address = other.Identity.Address
		}
		if record.Identity.DateOfBirth == ( clients.DateOfBirth{} ) {
			record.Identity.DateOfBirth = other.Identity.DateOfBirth
		}
		for _ , person := range other.AuthorizedAliases {
			if aliases[ alias_key( person ) ] { continue }
			aliases[ alias_key( person ) ] = true
			record.AuthorizedAliases = append( record.AuthorizedAliases , person )
		}
		if len( kept.Record.FamilyMembers ) == 0 && len( other.FamilyMembers ) > len( record.FamilyMembers ) {
			record.FamilyMembers = other.FamilyMembers
		}
		record.Verified = record.Verified || other.Verified
		if other.CreatedAt.IsZero() == false && ( record.CreatedAt.IsZero() || other.CreatedAt.Before( record.CreatedAt ) ) {
			record.CreatedAt = other.CreatedAt
		}
		for name , value := range other.Extra {
			if _ , has := extra[ name ]; has == false {
				extra[ name ] = value
			}
		}
		largest = max( largest , other.FamilySize )
	}
	if len( extra ) > 0 {
		record.Extra = extra
	}

	// What prepare compares against: every barcode is already the family's,
	// with the earliest date any record had for it, so a moved barcode keeps
	// its date instead of looking newly assigned.
	previous := clients.Client{ FamilySize: largest , Barcodes: record.Barcodes , BarcodeAssigned: assigned }
	if err = service.prepare( &record , &previous ); err != nil { return }

	for _ , source := range sources {
		for _ , entry := range service.Index.VisitsOf( source.ID ) {
			visit := entry.Visit
			visit.ClientID = keep
			if _ , err = service.Docs.Put( visits.Collection , entry.ID , visit ); err != nil { return }
		}
	}
	if _ , err = service.Docs.PutIf( clients.Collection , keep , record , kept.HLC ); err != nil {
		if errors.Is( err , docs.ErrConflict ) {
			err = ErrStale
		}
		return
	}
	for _ , source := range sources {
		if _ , err = service.Docs.Delete( clients.Collection , source.ID ); err != nil { return }
	}
	merged , _ = service.Index.Client( keep )
	return
}

// BarcodeRow is one barcode on one family, for the barcodes list.
type BarcodeRow struct {
	Barcode  string `json:"barcode"`
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
	// AssignedAt is zero when nothing tells when it was assigned.
	AssignedAt time.Time `json:"assigned_at"`
	// Estimated is a barcode from before assignment dates were recorded:
	// AssignedAt is the first visit that printed it, or else when the
	// family registered.
	Estimated bool `json:"estimated"`
	// Owners is how many families carry this barcode; more than one is a
	// duplicate to sort out.
	Owners int `json:"owners"`
}

// BarcodeRows is every barcode on every live family, most recently
// assigned first (undated ones last).
func ( service *Service ) BarcodeRows() ( rows []BarcodeRow ) {
	rows = []BarcodeRow{}
	owners := map[string]int{}
	for _ , client := range service.Index.Clients() {
		record := client.Record
		var first_printed map[string]time.Time
		for _ , barcode := range record.Barcodes {
			owners[ barcode ]++
			row := BarcodeRow{ Barcode: barcode , ClientID: client.ID , Name: record.Name() }
			if at , ok := record.BarcodeAssigned[ barcode ]; ok {
				row.AssignedAt = at
			} else {
				if first_printed == nil {
					first_printed = firstPrinted( service.Index.VisitsOf( client.ID ) )
				}
				row.Estimated = true
				row.AssignedAt = record.CreatedAt
				if at , ok := first_printed[ barcode ]; ok && ( row.AssignedAt.IsZero() || at.Before( row.AssignedAt ) ) {
					row.AssignedAt = at
				}
			}
			rows = append( rows , row )
		}
	}
	for index := range rows {
		rows[ index ].Owners = owners[ rows[ index ].Barcode ]
	}
	sort.SliceStable( rows , func( a int , b int ) bool {
		left , right := rows[ a ].AssignedAt , rows[ b ].AssignedAt
		if left.Equal( right ) {
			return rows[ a ].Barcode < rows[ b ].Barcode
		}
		if left.IsZero() || right.IsZero() {
			return right.IsZero()
		}
		return left.After( right )
	} )
	return
}

// firstPrinted is the earliest visit time each barcode was printed on.
func firstPrinted( history []visits.Entry ) ( first map[string]time.Time ) {
	first = map[string]time.Time{}
	for _ , entry := range history {
		ticket := entry.Visit.Ticket
		if ticket == nil || ticket.Barcode == "" { continue }
		if at , ok := first[ ticket.Barcode ]; ok == false || entry.Visit.At.Before( at ) {
			first[ ticket.Barcode ] = entry.Visit.At
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
