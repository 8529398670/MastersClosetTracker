// Command mct-site is the closet's public website: the landing page with the
// calendar and a family's offline phone pass (/#<barcode>), and the offline
// pre-signup form (/join).
//
// It serves static files and nothing else. There is no database, no account,
// no API: a family's barcode reaches the home page only after the "#" in the
// link on their ticket, which browsers never send to a server, and the
// pre-signup form turns into a QR code on the phone itself. So there is
// nothing here for anyone to probe, brute force, or flood with sign-ups.
//
//	mct-site                    run the site
//	mct-site config check       create, check, or read config.site.yaml
//	mct-site health             the container's healthcheck
//	mct-site version            build information
//
// --config <path> anywhere on the command line picks the config file.
package main

import (
	fmt "fmt"
	log "log"
	os "os"
	runtime "runtime"

	config "github.com/0187773933/MastersClosetTracker/server/config"
	healthcheck "github.com/0187773933/MastersClosetTracker/server/healthcheck"
)

// Stamped by build.sh via -ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	config.Use( config.Site )
	arguments , err := config.StripConfigFlag( os.Args[ 1: ] )
	if err != nil {
		fmt.Fprintf( os.Stderr , "error: %v\n" , err )
		os.Exit( 2 )
	}

	if len( arguments ) > 0 {
		switch arguments[ 0 ] {
		case "config":
			if err := config.RunCommand( arguments[ 1: ] ); err != nil {
				fmt.Fprintf( os.Stderr , "error: %v\n" , err )
				os.Exit( 1 )
			}
			return
		case "health":
			if err := healthcheck.Run(); err != nil {
				fmt.Fprintf( os.Stderr , "unhealthy: %v\n" , err )
				os.Exit( 1 )
			}
			return
		case "version" , "-version" , "--version":
			fmt.Printf( "Masters Closet Site %s (commit %s, built %s, %s/%s, %s)\n" ,
				version , commit , buildDate , runtime.GOOS , runtime.GOARCH , runtime.Version() )
			return
		case "-h" , "--help" , "help":
			usage()
			return
		}
	}

	if err := run(); err != nil {
		log.Fatalf( "[fatal] %v" , err )
	}
}

func usage() {
	fmt.Printf( "Masters Closet Site %s\n\nUsage:\n  mct-site [--config path]            run the site\n  mct-site config <cmd>               create, check, or read config.site.yaml\n  mct-site health                     exit 0 if the local site answers\n  mct-site version                    build information\n\n" , version )
	config.CommandUsage()
}
