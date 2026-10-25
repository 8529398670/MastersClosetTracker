// Package clients is the client record -- a family the closet serves -- and
// the rules that are about one record on its own: how names are normalised,
// how big a family may be, when an address makes someone verified, what a
// barcode looks like, and whether two records look like the same family.
//
// A client record holds the profile only. Visits are their own records
// (server/visits) and nothing about a visit is ever written back here, so a
// check-in at one station can never collide with a profile edit at another.
//
// The JSON names are the ones the current pages use. Anything else a page
// sends is kept as-is in Extra and sent back, so a page can start keeping a
// new field without a backend change -- except the derived fields the pages
// echo back from what they were shown (visits, balance, ...), which are
// dropped: those are computed, never stored.
package clients

import (
	json "encoding/json"
	strings "strings"
	time "time"
	unicode "unicode"

	cases "golang.org/x/text/cases"
	language "golang.org/x/text/language"
	runes "golang.org/x/text/runes"
	transform "golang.org/x/text/transform"
	norm "golang.org/x/text/unicode/norm"
)

// Collection is where client records live.
const Collection = "clients"

type Address struct {
	StreetNumber string `json:"street_number"`
	StreetName   string `json:"street_name"`
	AddressTwo   string `json:"address_two"`
	City         string `json:"city"`
	State        string `json:"state"`
	ZipCode      string `json:"zipcode"`
}

// DateOfBirth keeps the month as a name ("January"), as it was entered.
type DateOfBirth struct {
	Month string `json:"month"`
	Day   int    `json:"day"`
	Year  int    `json:"year"`
}

// Person is the account holder, a family member, or someone authorised to
// shop for the family.
type Person struct {
	FirstName   string      `json:"first_name"`
	LastName    string      `json:"last_name"`
	MiddleName  string      `json:"middle_name"`
	Address     Address     `json:"address"`
	DateOfBirth DateOfBirth `json:"date_of_birth"`
	Age         int         `json:"age"`
	Sex         string      `json:"sex"`
	Height      string      `json:"height"`
	EyeColor    string      `json:"eye_color"`
	Spouse      bool        `json:"spouse"`
}

// Client is the stored profile. The record id is the client's id; it is not
// repeated in the body.
type Client struct {
	Verified          bool      `json:"verified"`
	Barcodes          []string  `json:"barcodes"`
	// BarcodeAssigned is when each barcode was first put on this record.
	// Barcodes from before it was recorded have no entry.
	BarcodeAssigned   map[string]time.Time `json:"barcode_assigned,omitempty"`
	EmailAddress      string    `json:"email_address"`
	PhoneNumber       string    `json:"phone_number"`
	Identity          Person    `json:"identity"`
	AuthorizedAliases []Person  `json:"authorized_aliases"`
	FamilySize        int       `json:"family_size"`
	FamilyMembers     []Person  `json:"family_members"`
	Language          string    `json:"language"`
	CreatedAt         time.Time `json:"created_at"`

	// Extra is every other top-level key a page sent, kept verbatim.
	Extra map[string]json.RawMessage `json:"-"`
}

// stored are the keys Client itself owns.
var stored = map[string]bool{
	"verified": true , "barcodes": true , "email_address": true , "phone_number": true ,
	"identity": true , "authorized_aliases": true , "family_size": true , "family_members": true ,
	"language": true , "created_at": true , "barcode_assigned": true ,
}

// derived are keys the pages are shown and send back, which are computed
// from other records and must never be stored -- storing them would freeze
// a stale copy (the old system did exactly this with check-ins and balance).
var derived = map[string]bool{
	"uuid": true , "id": true , "_rev": true , "username": true , "name_string": true ,
	"search_string": true , "ulid": true , "spanish": true ,
	"created_date": true , "created_time": true , "modified_date": true , "modified_time": true ,
	"check_ins": true , "failed_check_ins": true , "total_guests_admitted": true ,
	"balance": true , "time_remaining": true , "allowed_to_checkin": true , "similar_users": true ,
	"last_check_in_date": true , "last_check_in_time": true , "check_in_count": true ,
}

type plainClient Client

func ( client Client ) MarshalJSON() ( result []byte , err error ) {
	result , err = json.Marshal( plainClient( client ) )
	if err != nil || len( client.Extra ) == 0 { return }
	var merged map[string]json.RawMessage
	if err = json.Unmarshal( result , &merged ); err != nil { return }
	for name , value := range client.Extra {
		if stored[ name ] || derived[ name ] { continue }
		merged[ name ] = value
	}
	result , err = json.Marshal( merged )
	return
}

func ( client *Client ) UnmarshalJSON( raw []byte ) ( err error ) {
	var plain plainClient
	if err = json.Unmarshal( raw , &plain ); err != nil { return }
	var all map[string]json.RawMessage
	if err = json.Unmarshal( raw , &all ); err != nil { return }
	plain.Extra = nil
	for name , value := range all {
		if stored[ name ] || derived[ name ] { continue }
		if plain.Extra == nil {
			plain.Extra = map[string]json.RawMessage{}
		}
		plain.Extra[ name ] = value
	}
	// Records from before language codes only said whether the family
	// speaks Spanish.
	if plain.Language == "" {
		var legacy struct {
			Spanish bool `json:"spanish"`
		}
		if json.Unmarshal( raw , &legacy ) == nil && legacy.Spanish {
			plain.Language = "es"
		}
	}
	*client = Client( plain )
	return
}

// ---------------------------------------------------------------------------
// names
// ---------------------------------------------------------------------------

var titleCaser = cases.Title( language.AmericanEnglish )

func titleCase( text string ) ( result string ) {
	result = titleCaser.String( strings.ToLower( strings.Join( strings.Fields( text ) , " " ) ) )
	return
}

func ( client *Client ) nameParts() ( parts []string ) {
	for _ , part := range []string{ client.Identity.FirstName , client.Identity.MiddleName , client.Identity.LastName } {
		if part != "" {
			parts = append( parts , part )
		}
	}
	return
}

// Name is the display name: "Maria Elena Lopez".
func ( client *Client ) Name() ( result string ) {
	result = strings.Join( client.nameParts() , " " )
	return
}

// Username is the hyphenated form the pages also show: "Maria-Elena-Lopez".
func ( client *Client ) Username() ( result string ) {
	result = strings.Join( strings.Fields( client.Name() ) , "-" )
	return
}

var folder = transform.Chain( norm.NFD , runes.Remove( runes.In( unicode.Mn ) ) , norm.NFC )

// Fold lowercases and strips accents, so "José" and "jose" are the same word
// to search.
func Fold( text string ) ( result string ) {
	folded , _ , err := transform.String( folder , text )
	if err != nil {
		folded = text
	}
	result = strings.ToLower( folded )
	return
}

// SearchTokens are the folded words a name search matches against: the
// holder's names and the names of anyone authorised to shop for them.
func ( client *Client ) SearchTokens() ( tokens []string ) {
	seen := map[string]bool{}
	add := func( text string ) {
		for _ , word := range strings.FieldsFunc( Fold( text ) , func( r rune ) bool { return unicode.IsLetter( r ) == false && unicode.IsDigit( r ) == false } ) {
			if seen[ word ] == false {
				seen[ word ] = true
				tokens = append( tokens , word )
			}
		}
	}
	for _ , person := range append( []Person{ client.Identity } , client.AuthorizedAliases... ) {
		add( person.FirstName )
		add( person.MiddleName )
		add( person.LastName )
	}
	return
}

// ---------------------------------------------------------------------------
// normalising a record before it is saved
// ---------------------------------------------------------------------------

// Normalize tidies a record the way every save should: names title-cased,
// stray whitespace gone, barcodes trimmed and de-duplicated, and a family
// size no smaller than the people actually listed.
func Normalize( client *Client ) {
	identity := &client.Identity
	identity.FirstName = titleCase( identity.FirstName )
	identity.MiddleName = titleCase( identity.MiddleName )
	identity.LastName = titleCase( identity.LastName )
	identity.Address.ZipCode = strings.TrimSpace( identity.Address.ZipCode )
	client.EmailAddress = strings.TrimSpace( client.EmailAddress )
	client.PhoneNumber = strings.TrimSpace( client.PhoneNumber )
	client.Language = strings.ToLower( strings.TrimSpace( client.Language ) )

	seen := map[string]bool{}
	barcodes := []string{}
	for _ , barcode := range client.Barcodes {
		barcode = strings.TrimSpace( barcode )
		if barcode == "" || seen[ barcode ] { continue }
		seen[ barcode ] = true
		barcodes = append( barcodes , barcode )
	}
	client.Barcodes = barcodes

	if minimum := len( client.FamilyMembers ) + 1; client.FamilySize < minimum {
		client.FamilySize = minimum
	}
}

// AutoVerify marks a client verified when their zipcode is the configured
// local one. It only ever turns verification on.
func AutoVerify( client *Client , zipcode string ) {
	if zipcode != "" && client.Identity.Address.ZipCode == zipcode {
		client.Verified = true
	}
}

// ClampFamily caps a family at max people, counting the account holder (0
// means no cap) -- but never below what the record already had. Growth past
// the cap is refused; a family that was already larger (created before the
// cap, or before it was lowered) keeps every member. Family size multiplies
// every allowance, which is why this is enforced here and not only in the
// page.
func ClampFamily( client *Client , max int , existing_size int ) {
	if max <= 0 { return }
	allowed := max
	if existing_size > allowed {
		allowed = existing_size
	}
	if len( client.FamilyMembers ) > allowed-1 {
		client.FamilyMembers = client.FamilyMembers[ :allowed-1 ]
	}
	if client.FamilySize > allowed {
		client.FamilySize = allowed
	}
}
