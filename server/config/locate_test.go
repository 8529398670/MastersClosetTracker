package config

import (
	filepath "path/filepath"
	testing "testing"
)

// A config in a checkout's configs/ resolves ./static against the project
// directory; one anywhere else resolves it beside itself.
func TestPathBase( t *testing.T ) {
	project := filepath.Join( "/srv" , "mct" )
	if got := pathBase( filepath.Join( project , LocalConfigDir , "config.yaml" ) ); got != project {
		t.Fatalf( "configs/ config: base %q, want %q" , got , project )
	}
	if got := pathBase( filepath.Join( project , "config.yaml" ) ); got != project {
		t.Fatalf( "config beside the binary: base %q, want %q" , got , project )
	}
}
