package config

import (
	testing "testing"
)

// A config.yaml written while phones still held signed passes keeps loading:
// the retired guests keys are accepted and only named for a warning.
func TestRetiredGuestKeysStillLoad( t *testing.T ) {
	raw := []byte( "guests:\n" +
		"  public_url: \"https://masterscloset.cc\"\n" +
		"  signing_key: \"fac1cf1e78eadd84d1e84f6faa77e28b49d3e978e18ed8954a9c04b7abf18ca6\"\n" +
		"  cookie_name: guest\n" +
		"  claim_ttl_hours: 168\n" +
		"  pass_ttl_days: 365\n" )
	file , err := ParseAs( Station , raw , "old config.yaml" )
	if err != nil {
		t.Fatalf( "an old guests section should still parse: %v" , err )
	}
	if file.Guests.PublicURL != "https://masterscloset.cc" {
		t.Fatalf( "public_url = %q" , file.Guests.PublicURL )
	}
	if retired := file.Guests.retired(); len( retired ) != 4 {
		t.Fatalf( "retired keys = %v, want all four" , retired )
	}
}

// The site's own config parses, and a station section in it is refused.
func TestSiteProfileSections( t *testing.T ) {
	if _ , err := ParseAs( Site , []byte( "site:\n  ical_url: \"https://example.org/x.ics\"\n" ) , "site" ); err != nil {
		t.Fatalf( "site config: %v" , err )
	}
	if _ , err := ParseAs( Site , []byte( "guests:\n  public_url: \"\"\n" ) , "site" ); err == nil {
		t.Fatal( "a guests section in the site config should be refused" )
	}
	for _ , sibling := range func() []*Profile { Use( Site ); defer Use( Station ); return Siblings() }() {
		if sibling == Site {
			t.Fatal( "Siblings must not include the active profile" )
		}
	}
}
