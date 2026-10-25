package sitefiles

import (
	fs "io/fs"
	testing "testing"
)

// The public site serves its own pages and the few shared assets -- never a
// staff page -- and everything it links to is actually embedded.
func TestAllowedFiles( t *testing.T ) {
	for _ , name := range []string{ "site/index.html" , "site/pass.html" , "site/join.html" , "site/sw.js" , "app/js/qr.js" , "vendor/JsBarcode.all.min.js" } {
		if Allowed( name ) == false {
			t.Errorf( "%s should be served" , name )
		}
		if _ , err := fs.Stat( Files , name ); err != nil {
			t.Errorf( "%s is not embedded: %v" , name , err )
		}
	}
	for _ , name := range []string{ "app/pages/admin.html" , "account.html" , "app/js/pages/admin_checkin.js" , "sitefiles.go" , "site/../account.html" } {
		if Allowed( name ) {
			t.Errorf( "%s must not be served" , name )
		}
	}
}
