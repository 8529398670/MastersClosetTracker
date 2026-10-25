// Package sitefiles is the public site's copy of the frontend (cmd/site): the
// site's own pages under site/, plus the handful of shared assets they load.
//
// It lives in static/ only because go:embed cannot reach outside its own
// directory. The list below is the site's whole world -- the staff pages are
// not embedded here, so the public site cannot serve them even by mistake.
// cmd/site applies the same list to a ./static on disk (Allowed).
package sitefiles

import (
	embed "embed"
	strings "strings"
)

//go:embed all:site app/css/app.css app/img/logo.png app/img/verified.png app/img/favicon.ico app/js/qr.js app/js/presignup.js app/js/field_labels.js vendor/qr-code-styling.min.js vendor/JsBarcode.all.min.js
var Files embed.FS

// shared are the files outside site/ that the site's pages load.
var shared = map[string]bool{
	"app/css/app.css":                 true,
	"app/img/logo.png":                true,
	"app/img/verified.png":            true,
	"app/img/favicon.ico":             true,
	"app/js/qr.js":                    true,
	"app/js/presignup.js":             true,
	"app/js/field_labels.js":          true,
	"vendor/qr-code-styling.min.js":   true,
	"vendor/JsBarcode.all.min.js":     true,
}

// Allowed reports whether name (slash-separated, relative to static/) is
// one of the site's files.
func Allowed( name string ) ( result bool ) {
	result = shared[ name ] || ( strings.HasPrefix( name , "site/" ) && strings.Contains( name , ".." ) == false )
	return
}
