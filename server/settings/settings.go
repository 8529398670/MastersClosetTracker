// Package settings is everything the closet can change from the Settings
// page, in two scopes:
//
//   - org settings (allowances, cooloff, time zone, ...) are the same at every
//     station. Each one is its own synced record (collection "settings", id =
//     the setting's path), so two admins changing two different settings on
//     two stations at once both keep their change.
//   - node settings (the label printer) belong to one machine and never sync.
//
// Infrastructure -- ports, keys, the hub address -- is config.yaml's job, not
// this package's; the Settings page shows some of it read-only.
//
// A setting nobody has changed has no record at all and reads as its
// default. That is deliberate: a freshly installed station that wrote its
// defaults out as records would stamp them newer than the closet's real
// values and overwrite them everywhere on its first sync.
package settings

import (
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	math "math"
	regexp "regexp"
	sort "sort"
	strconv "strconv"
	strings "strings"
	sync "sync"
	time "time"

	config "github.com/0187773933/MastersClosetTracker/server/config"
	db "github.com/0187773933/MastersClosetTracker/server/db"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
)

const (
	ScopeOrg    = "org"
	ScopeNode   = "node"
	ScopeConfig = "config"

	KindInt    = "int"
	KindFloat  = "float"
	KindString = "string"
	KindBool   = "bool"

	// Collection holds org settings, one record per path.
	Collection = "settings"

	// BucketNode holds this machine's settings, path -> value.
	BucketNode = "node_settings"
)

var Buckets = []string{ BucketNode }

// Field describes one setting for the Settings page, which renders whatever
// list it is given. Only the json-tagged parts reach the browser.
type Field struct {
	Path            string `json:"path"`
	Label           string `json:"label"`
	Section         string `json:"section"`
	Kind            string `json:"kind"`
	Scope           string `json:"scope"`
	Secret          bool   `json:"secret"`
	RestartRequired bool   `json:"restart_required"`
	ReadOnly        bool   `json:"read_only"`
	Help            string `json:"help,omitempty"`

	def   any
	check func( value any ) error
}

var (
	zipPattern = regexp.MustCompile( `^[0-9]{5}$` )
	urlPattern = regexp.MustCompile( `^https?://\S+$` )
)

func intRange( low int , high int ) ( check func( any ) error ) {
	check = func( value any ) ( err error ) {
		number := value.( int )
		if number < low || number > high {
			err = fmt.Errorf( "must be between %d and %d" , low , high )
		}
		return
	}
	return
}

func floatRange( low float64 , high float64 ) ( check func( any ) error ) {
	check = func( value any ) ( err error ) {
		number := value.( float64 )
		if number < low || number > high {
			err = fmt.Errorf( "must be between %g and %g" , low , high )
		}
		return
	}
	return
}

// schema is every editable setting, in the order the page shows them. A
// section appears where its first field does, so keep each section's rows
// together.
var schema = []Field{
	{ Path: "max_family_size" , Label: "Max Family Size" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 0 , check: intRange( 0 , 50 ) ,
		Help: "Largest family an account may have, counting the account holder. 0 = no limit." },
	{ Path: "balance.general.total" , Label: "Clothing Items per Person" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 6 , check: intRange( 0 , 100 ) ,
		Help: "The default on the check-in ticket." },
	{ Path: "balance.general.tops" , Label: "Tops per Person" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 6 , check: intRange( 0 , 100 ) },
	{ Path: "balance.general.bottoms" , Label: "Bottoms per Person" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 3 , check: intRange( 0 , 100 ) },
	{ Path: "balance.general.dresses" , Label: "Dresses per Person" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 1 , check: intRange( 0 , 100 ) },
	{ Path: "balance.shoes" , Label: "Shoes per Person" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 1 , check: intRange( 0 , 100 ) },
	{ Path: "balance.seasonals" , Label: "Seasonals per Person" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 1 , check: intRange( 0 , 100 ) },
	{ Path: "balance.accessories" , Label: "Accessories per Person" , Section: "Allowance" , Kind: KindInt , Scope: ScopeOrg , def: 2 , check: intRange( 0 , 100 ) },

	{ Path: "check_in_cooloff_days" , Label: "Check-In Cooloff (days)" , Section: "Check-In" , Kind: KindInt , Scope: ScopeOrg , def: 30 , check: intRange( 0 , 365 ) ,
		Help: "Days a family waits between visits. Also the window the balance is counted over." },
	{ Path: "similarity_threshold" , Label: "Similarity Threshold" , Section: "Check-In" , Kind: KindInt , Scope: ScopeOrg , def: 3 , check: intRange( 0 , 10 ) ,
		Help: "How many letters two names may differ by and still be flagged as a possible duplicate." },
	{ Path: "auto_verify_zipcode" , Label: "Auto-Verify Zipcode" , Section: "Check-In" , Kind: KindString , Scope: ScopeOrg , def: "45424" ,
		Help: "Clients with this zipcode are marked Verified when saved. Empty turns it off." ,
		check: func( value any ) ( err error ) {
			if text := value.( string ); text != "" && zipPattern.MatchString( text ) == false {
				err = errors.New( "must be five digits or empty" )
			}
			return
		} },
	{ Path: "time_zone" , Label: "Time Zone" , Section: "Check-In" , Kind: KindString , Scope: ScopeOrg , def: "America/New_York" ,
		Help: "Decides which day a visit counts toward. An IANA name like America/New_York." ,
		check: func( value any ) ( err error ) {
			if _ , load_err := time.LoadLocation( value.( string ) ); load_err != nil || value.( string ) == "" {
				err = errors.New( "is not a known time zone (try America/New_York)" )
			}
			return
		} },
	{ Path: "ical_url" , Label: "Public Calendar URL" , Section: "Check-In" , Kind: KindString , Scope: ScopeOrg ,
		def: "https://calendar.google.com/calendar/ical/masters.closet.5950%40gmail.com/public/basic.ics" ,
		Help: "Where /ical sends people." ,
		check: func( value any ) ( err error ) {
			if text := value.( string ); text != "" && urlPattern.MatchString( text ) == false {
				err = errors.New( "must be an http(s) URL or empty" )
			}
			return
		} },

	{ Path: "ticket.handoff_qr" , Label: "Print Phone Pass QR on Ticket" , Section: "Ticket" , Kind: KindBool , Scope: ScopeOrg , def: false ,
		Help: "Prints a QR code beside the barcode. A phone that scans it keeps the family's barcode as an offline pass to show at the front desk. Needs guests.public_url in config.yaml." },

	{ Path: "printer.printer_name" , Label: "Printer Name" , Section: "Printer (this station)" , Kind: KindString , Scope: ScopeNode , def: "" ,
		Help: "The label printer's queue name. Empty renders tickets without printing them." },
	{ Path: "printer.speed" , Label: "Print Speed" , Section: "Printer (this station)" , Kind: KindInt , Scope: ScopeNode , def: 2 , check: intRange( 1 , 20 ) },
	{ Path: "printer.page_width" , Label: "Page Width (in)" , Section: "Printer (this station)" , Kind: KindFloat , Scope: ScopeNode , def: 4.0 , check: floatRange( 1 , 20 ) },
	{ Path: "printer.page_height" , Label: "Page Height (in)" , Section: "Printer (this station)" , Kind: KindFloat , Scope: ScopeNode , def: 6.0 , check: floatRange( 1 , 20 ) },
	{ Path: "printer.font_name" , Label: "Ticket Font" , Section: "Printer (this station)" , Kind: KindString , Scope: ScopeNode , def: "ComicNeue" },
	{ Path: "printer.sumatra_path" , Label: "SumatraPDF Path" , Section: "Printer (this station)" , Kind: KindString , Scope: ScopeNode , def: "" ,
		Help: "Windows only. Blank looks beside the app, in the app folder, the working directory, then PATH." },

	{ Path: "station.name" , Label: "Station Name" , Section: "This Station" , Kind: KindString , Scope: ScopeNode , def: "" , RestartRequired: true ,
		Help: "Labels the visits this station records and its sync status. Blank uses this computer's name, login and hardware ID." },
}

func lookup( path string ) ( field Field , ok bool ) {
	for _ , candidate := range schema {
		if candidate.Path == path {
			field , ok = candidate , true
			return
		}
	}
	return
}

// Balance is the per-person allowance.
type Balance struct {
	General     BalanceGeneral `json:"general"`
	Shoes       int            `json:"shoes"`
	Seasonals   int            `json:"seasonals"`
	Accessories int            `json:"accessories"`
}

type BalanceGeneral struct {
	Total   int `json:"total"`
	Tops    int `json:"tops"`
	Bottoms int `json:"bottoms"`
	Dresses int `json:"dresses"`
}

// Org is the effective org settings at one moment.
type Org struct {
	MaxFamilySize       int
	Balance             Balance
	CooloffDays         int
	SimilarityThreshold int
	AutoVerifyZipcode   string
	TimeZone            string
	ICalURL             string
	HandoffQR           bool
	Location            *time.Location
}

// Printer is this station's label printer.
type Printer struct {
	Name        string
	Speed       int
	PageWidth   float64
	PageHeight  float64
	FontName    string
	SumatraPath string
}

// Service holds the explicit values (records and node bucket) and answers
// with defaults for the rest.
type Service struct {
	docs  *docs.Store
	store *db.Store
	cfg   *config.Config

	mu   sync.RWMutex
	org  map[string]any
	node map[string]any

	// auto is the machine-derived station name, read once: on macOS and
	// Windows it means running a system command.
	auto     string
	autoOnce sync.Once
}

type orgRecord struct {
	Value json.RawMessage `json:"value"`
}

// New loads every explicit value and starts following synced changes.
func New( docs_store *docs.Store , store *db.Store , cfg *config.Config ) ( service *Service , err error ) {
	service = &Service{ docs: docs_store , store: store , cfg: cfg , org: map[string]any{} , node: map[string]any{} }

	err = docs_store.ForEach( Collection , func( envelope docs.Envelope ) bool {
		service.absorb( envelope )
		return true
	} )
	if err != nil { return }
	docs_store.Hook( Collection , func( current docs.Envelope , _ *docs.Envelope ) {
		service.absorb( current )
	} )

	err = store.ForEach( BucketNode , func() any { return new( json.RawMessage ) } , func( key []byte , item any ) bool {
		if field , ok := lookup( string( key ) ); ok {
			if value , decode_err := decodeValue( field , *item.( *json.RawMessage ) ); decode_err == nil {
				service.node[ field.Path ] = value
			}
		}
		return true
	} )
	return
}

// absorb takes one synced setting into the cache. A path this version does
// not know (a newer station added it) is ignored but stays in the store, so
// it is still there after this station is upgraded.
func ( service *Service ) absorb( envelope docs.Envelope ) {
	field , ok := lookup( envelope.ID )
	if ok == false || field.Scope != ScopeOrg { return }
	service.mu.Lock()
	defer service.mu.Unlock()
	if envelope.Deleted {
		delete( service.org , field.Path )
		return
	}
	var record orgRecord
	if json.Unmarshal( envelope.Body , &record ) != nil { return }
	value , err := decodeValue( field , record.Value )
	if err != nil { return }
	service.org[ field.Path ] = value
}

func decodeValue( field Field , raw json.RawMessage ) ( value any , err error ) {
	var loose any
	if err = json.Unmarshal( raw , &loose ); err != nil { return }
	value , err = coerce( field , loose )
	return
}

// coerce turns what the Settings page sent (always strings) or what a
// record holds (JSON numbers) into the field's Go type, and validates it.
func coerce( field Field , raw any ) ( value any , err error ) {
	switch field.Kind {
	case KindInt:
		var number float64
		switch typed := raw.( type ) {
		case float64:
			number = typed
		case string:
			number , err = strconv.ParseFloat( strings.TrimSpace( typed ) , 64 )
		case bool , nil:
			err = errors.New( "must be a whole number" )
		}
		if err != nil || number != math.Trunc( number ) {
			err = errors.New( "must be a whole number" )
			return
		}
		value = int( number )
	case KindFloat:
		var number float64
		switch typed := raw.( type ) {
		case float64:
			number = typed
		case string:
			number , err = strconv.ParseFloat( strings.TrimSpace( typed ) , 64 )
		default:
			err = errors.New( "must be a number" )
		}
		if err != nil {
			err = errors.New( "must be a number" )
			return
		}
		value = number
	case KindBool:
		switch typed := raw.( type ) {
		case bool:
			value = typed
		case string:
			value , err = strconv.ParseBool( strings.TrimSpace( typed ) )
		default:
			err = errors.New( "must be true or false" )
		}
	default:
		text , ok := raw.( string )
		if ok == false {
			err = errors.New( "must be text" )
			return
		}
		value = strings.TrimSpace( text )
	}
	if err == nil && field.check != nil {
		err = field.check( value )
	}
	return
}

func ( service *Service ) value( path string ) ( value any ) {
	field , _ := lookup( path )
	service.mu.RLock()
	defer service.mu.RUnlock()
	source := service.org
	if field.Scope == ScopeNode {
		source = service.node
	}
	value , ok := source[ path ]
	if ok == false {
		value = field.def
	}
	return
}

func ( service *Service ) intValue( path string ) ( result int ) {
	result , _ = service.value( path ).( int )
	return
}

func ( service *Service ) floatValue( path string ) ( result float64 ) {
	result , _ = service.value( path ).( float64 )
	return
}

func ( service *Service ) stringValue( path string ) ( result string ) {
	result , _ = service.value( path ).( string )
	return
}

func ( service *Service ) boolValue( path string ) ( result bool ) {
	result , _ = service.value( path ).( bool )
	return
}

// Org is the effective org settings.
func ( service *Service ) Org() ( org Org ) {
	org = Org{
		MaxFamilySize: service.intValue( "max_family_size" ),
		Balance: Balance{
			General: BalanceGeneral{
				Total:   service.intValue( "balance.general.total" ),
				Tops:    service.intValue( "balance.general.tops" ),
				Bottoms: service.intValue( "balance.general.bottoms" ),
				Dresses: service.intValue( "balance.general.dresses" ),
			},
			Shoes:       service.intValue( "balance.shoes" ),
			Seasonals:   service.intValue( "balance.seasonals" ),
			Accessories: service.intValue( "balance.accessories" ),
		},
		CooloffDays:         service.intValue( "check_in_cooloff_days" ),
		SimilarityThreshold: service.intValue( "similarity_threshold" ),
		AutoVerifyZipcode:   service.stringValue( "auto_verify_zipcode" ),
		TimeZone:            service.stringValue( "time_zone" ),
		ICalURL:             service.stringValue( "ical_url" ),
		HandoffQR:           service.boolValue( "ticket.handoff_qr" ),
	}
	location , err := time.LoadLocation( org.TimeZone )
	if err != nil {
		location = time.UTC
	}
	org.Location = location
	return
}

// Printer is this station's printer settings.
func ( service *Service ) Printer() ( printer Printer ) {
	printer = Printer{
		Name:        service.stringValue( "printer.printer_name" ),
		Speed:       service.intValue( "printer.speed" ),
		PageWidth:   service.floatValue( "printer.page_width" ),
		PageHeight:  service.floatValue( "printer.page_height" ),
		FontName:    service.stringValue( "printer.font_name" ),
		SumatraPath: service.stringValue( "printer.sumatra_path" ),
	}
	return
}

// StationName is what this station calls itself, first match wins: the
// name set on this station's Settings page, then sync.node_name in
// config.yaml, then one worked out from the machine. config.yaml is shared
// by every station when it lives in a synced folder, so the page setting --
// stored on this machine only -- is the one that tells stations apart.
func ( service *Service ) StationName() ( name string ) {
	name , _ = service.stationName()
	return
}

func ( service *Service ) stationName() ( name string , source string ) {
	if name = service.stringValue( "station.name" ); name != "" {
		source = "set on this station's Settings page"
		return
	}
	if name = service.cfg.SyncNodeName; name != "" {
		source = "sync.node_name in config.yaml, shared by every station using that file"
		return
	}
	service.autoOnce.Do( func() {
		service.auto = config.AutoStationName( service.docs.Node() )
	} )
	name = service.auto
	source = "worked out from this computer's name, login and hardware ID"
	return
}

// configFields are config.yaml values shown read-only so whoever is at the
// station can see how it is set up without opening a terminal. Secrets are
// never among them.
func ( service *Service ) configFields() ( fields []Field , values map[string]any ) {
	station_name , name_source := service.stationName()
	fields = []Field{
		{ Path: "station.node_name" , Label: "Station Name in Use" , Help: "Where it comes from: " + name_source + ". A change takes effect on restart." },
		{ Path: "station.hub_url" , Label: "Hub" , Help: "sync.hub_url in config.yaml. Empty means this station does not sync." },
		{ Path: "station.public_url" , Label: "Public Site Address" , Help: "guests.public_url in config.yaml: the public site the ticket QR code links to. Empty prints no QR code." },
		{ Path: "station.node_id" , Label: "Database ID" },
		{ Path: "station.config_file" , Label: "Config File" },
		{ Path: "station.app_dir" , Label: "Data Folder" },
	}
	for index := range fields {
		fields[ index ].Section = "This Station"
		fields[ index ].Kind = KindString
		fields[ index ].Scope = ScopeConfig
		fields[ index ].ReadOnly = true
	}
	values = map[string]any{
		"station.node_name":   station_name,
		"station.hub_url":     service.cfg.SyncHubURL,
		"station.public_url":  service.cfg.SitePublicURL,
		"station.node_id":     service.docs.Node(),
		"station.config_file": service.cfg.ConfigFile,
		"station.app_dir":     service.cfg.AppDir,
	}
	return
}

// Fields is the whole list the Settings page renders.
func ( service *Service ) Fields() ( fields []Field ) {
	fields = append( fields , schema... )
	extra , _ := service.configFields()
	fields = append( fields , extra... )
	return
}

// Values is every field's effective value, keyed by path.
func ( service *Service ) Values() ( values map[string]any ) {
	values = map[string]any{}
	for _ , field := range schema {
		values[ field.Path ] = service.value( field.Path )
	}
	_ , extra := service.configFields()
	for path , value := range extra {
		values[ path ] = value
	}
	return
}

// Normalize coerces and validates one value for path the way a save from
// the Settings page would. ok is false for an unknown path or a bad value.
func Normalize( path string , raw any ) ( value any , ok bool ) {
	field , found := lookup( path )
	if found == false { return }
	value , err := coerce( field , raw )
	ok = err == nil
	return
}

// Default is a setting's value when nobody has set it.
func Default( path string ) ( value any ) {
	field , _ := lookup( path )
	value = field.def
	return
}

// FieldError names the setting that was refused, so the page can say which.
type FieldError struct {
	Path  string
	Label string
	Err   error
}

func ( field_error *FieldError ) Error() ( message string ) {
	message = fmt.Sprintf( "%s %v" , field_error.Label , field_error.Err )
	return
}

// Update applies edits from the Settings page: every field arrives, changed
// or not. Each is validated first -- one bad value saves nothing -- and then
// only values that differ from what is in effect are written, so saving the
// page untouched writes no records at all.
func ( service *Service ) Update( edits map[string]any ) ( restart_required []string , err error ) {
	type change struct {
		field Field
		value any
	}
	var changes []change
	paths := make( []string , 0 , len( edits ) )
	for path := range edits {
		paths = append( paths , path )
	}
	sort.Strings( paths )
	for _ , path := range paths {
		field , ok := lookup( path )
		if ok == false {
			// Read-only and config fields are shown but never sent back;
			// anything else unknown is a mistake worth refusing loudly.
			if strings.HasPrefix( path , "station." ) { continue }
			err = &FieldError{ Path: path , Label: path , Err: errors.New( "is not a setting" ) }
			return
		}
		value , coerce_err := coerce( field , edits[ path ] )
		if coerce_err != nil {
			err = &FieldError{ Path: path , Label: field.Label , Err: coerce_err }
			return
		}
		if fmt.Sprint( value ) == fmt.Sprint( service.value( path ) ) {
			continue
		}
		changes = append( changes , change{ field , value } )
	}
	for _ , item := range changes {
		raw , _ := json.Marshal( item.value )
		switch item.field.Scope {
		case ScopeOrg:
			_ , err = service.docs.Put( Collection , item.field.Path , orgRecord{ Value: raw } )
		case ScopeNode:
			if err = service.store.Put( BucketNode , []byte( item.field.Path ) , json.RawMessage( raw ) ); err == nil {
				service.mu.Lock()
				service.node[ item.field.Path ] = item.value
				service.mu.Unlock()
			}
		}
		if err != nil { return }
		if item.field.RestartRequired {
			restart_required = append( restart_required , item.field.Label )
		}
	}
	return
}
