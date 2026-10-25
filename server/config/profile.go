package config

import (
	_ "embed"
	fmt "fmt"
	filepath "path/filepath"
	strings "strings"
)

// Profile is everything that differs between the three binaries this module
// builds: the station (`mct`, the app the closet runs), the hub (`mct-hub`,
// the relay stations sync through) and the public site (`mct-site`, the
// static pages families see). All read config through this one package --
// one parser, one set of validation rules -- but each has its own defaults,
// its own sections, and its own place on disk, so two of them on the same
// machine never share a database or a config file by accident.
//
// A binary picks its profile once, first thing in main, with Use.
type Profile struct {
	// Name is the display name used in messages.
	Name string

	// Binary is the command name used in help text.
	Binary string

	// Slug names the default app directory, ~/.config/<slug>/.
	Slug string

	// LocalFileName is the config file looked for in the working directory
	// (a source checkout), and the default for `config init`. The two
	// profiles use different names so both can live in one checkout.
	LocalFileName string

	// ExampleFileName is the committed, keyless copy of the defaults.
	ExampleFileName string

	// Defaults is the embedded defaults file: every key, documented.
	Defaults []byte

	// Sections are the top-level keys a config file of this profile may
	// contain. A section that belongs to another binary is an error rather
	// than silently ignored -- it almost always means the wrong file.
	Sections []string

	// Stateless binaries keep no data, so they need no secret key.
	Stateless bool
}

//go:embed defaults.yaml
var stationDefaultsYAML []byte

//go:embed defaults_hub.yaml
var hubDefaultsYAML []byte

//go:embed defaults_site.yaml
var siteDefaultsYAML []byte

// Station is the app itself: the UI, the data, the printer.
var Station = &Profile{
	Name:            "Masters Closet Tracker",
	Binary:          "mct",
	Slug:            "masters-closet-tracker",
	LocalFileName:   "config.yaml",
	ExampleFileName: "config.example.yaml",
	Defaults:        stationDefaultsYAML,
	Sections:        []string{ "app" , "server" , "auth" , "guests" , "sync" , "docker" , "build" },
}

// Hub is the sync relay. It has no UI and no accounts, so it has no auth or
// guests sections; it has a hub section instead.
var Hub = &Profile{
	Name:            "Masters Closet Hub",
	Binary:          "mct-hub",
	Slug:            "mct-hub",
	LocalFileName:   "config.hub.yaml",
	ExampleFileName: "config.hub.example.yaml",
	Defaults:        hubDefaultsYAML,
	Sections:        []string{ "app" , "server" , "hub" , "docker" , "build" },
}

// Site is the public website: the landing page with the calendar, the
// family's offline phone pass, and the offline pre-signup form. It serves
// static files only -- no data, no accounts, no API -- so it has no secret.
var Site = &Profile{
	Name:            "Masters Closet Site",
	Binary:          "mct-site",
	Slug:            "mct-site",
	LocalFileName:   "config.site.yaml",
	ExampleFileName: "config.site.example.yaml",
	Defaults:        siteDefaultsYAML,
	Sections:        []string{ "app" , "server" , "site" , "docker" , "build" },
	Stateless:       true,
}

var profiles = []*Profile{ Station , Hub , Site }

var active = Station

// Use selects the profile for this process. Call it before anything else in
// this package.
func Use( profile *Profile ) {
	active = profile
}

// Active is the profile this process selected.
func Active() ( profile *Profile ) {
	profile = active
	return
}

// Siblings are the other profiles -- the ones whose configs may share a
// checkout or a host with this one.
func Siblings() ( result []*Profile ) {
	for _ , profile := range profiles {
		if profile != active {
			result = append( result , profile )
		}
	}
	return
}

func ( profile *Profile ) has( section string ) ( result bool ) {
	for _ , name := range profile.Sections {
		if name == section {
			result = true
			return
		}
	}
	return
}

// flagFile is the config named by --config, if any. It sits between the
// container's pinned path and the working-directory lookup.
var flagFile string

// StripConfigFlag removes a leading-or-anywhere `--config <path>` (or
// `--config=<path>`, `-config ...`) from arguments and pins that file. It
// returns the remaining arguments in order, so the subcommand switch in main
// never sees the flag and every subcommand -- serve, manage, config -- uses
// the same file.
func StripConfigFlag( arguments []string ) ( rest []string , err error ) {
	for index := 0; index < len( arguments ); index++ {
		argument := arguments[ index ]
		switch {
		case argument == "--config" || argument == "-config":
			if index+1 >= len( arguments ) {
				err = fmt.Errorf( "%s needs a path" , argument )
				return
			}
			flagFile = arguments[ index+1 ]
			index++
		case strings.HasPrefix( argument , "--config=" ) || strings.HasPrefix( argument , "-config=" ):
			flagFile = argument[ strings.Index( argument , "=" )+1: ]
		default:
			rest = append( rest , argument )
		}
	}
	if flagFile != "" {
		if absolute , abs_err := filepath.Abs( flagFile ); abs_err == nil {
			flagFile = absolute
		}
	}
	return
}

// flagDataDir is where state goes for a --config file that sets no app_dir:
// next to the file, named after it (config.b.yaml -> config.b.data/). Each
// config file then gets its own database without anyone having to remember
// to set app_dir, which is what lets several local nodes run side by side.
func flagDataDir( path string ) ( dir string ) {
	base := filepath.Base( path )
	base = strings.TrimSuffix( base , filepath.Ext( base ) )
	dir = filepath.Join( filepath.Dir( path ) , base+".data" )
	return
}
