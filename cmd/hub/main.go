// Command mct-hub is the Masters Closet sync hub: the relay every station
// pushes its changes to and pulls everyone else's from.
//
// It is deliberately small and knows nothing about clients or visits. It
// stores the latest copy of each record it is sent, numbers every change, and
// hands a station everything after the number it last saw. Conflicts are
// settled by the stamp each record carries (see server/hlc), never by
// reading the record -- which is what lets the stations' data model grow
// without the hub ever changing.
//
//	mct-hub                     run the hub
//	mct-hub keys create -name   make a station key (printed once)
//	mct-hub config check        create, check, or read config.hub.yaml
//	mct-hub health              the container's healthcheck
//	mct-hub version             build information
//
// --config <path> anywhere on the command line picks the config file.
package main

import (
	fmt "fmt"
	log "log"
	os "os"
	runtime "runtime"
	_ "time/tzdata"

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
	config.Use( config.Hub )
	arguments , err := config.StripConfigFlag( os.Args[ 1: ] )
	if err != nil {
		fmt.Fprintf( os.Stderr , "error: %v\n" , err )
		os.Exit( 2 )
	}

	if len( arguments ) > 0 {
		switch arguments[ 0 ] {
		case "keys" , "import" , "status":
			var command_err error
			switch arguments[ 0 ] {
			case "keys":
				command_err = runKeys( arguments[ 1: ] )
			case "import":
				command_err = runImport( arguments[ 1: ] )
			default:
				command_err = runStatus()
			}
			if command_err != nil {
				fmt.Fprintf( os.Stderr , "error: %v\n" , command_err )
				os.Exit( 1 )
			}
			return
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
			fmt.Printf( "Masters Closet Hub %s (commit %s, built %s, %s/%s, %s)\n" ,
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
	fmt.Printf( "Masters Closet Hub %s\n\nUsage:\n  mct-hub [--config path]             run the hub\n  mct-hub config <cmd>                create, check, or read config.hub.yaml\n  mct-hub health                      exit 0 if the local hub answers\n  mct-hub version                     build information\n\n" , version )
	fmt.Print( adminUsage )
	fmt.Println( "" )
	config.CommandUsage()
}
