package main

import (
	flag "flag"
	fmt "fmt"
	os "os"
	strings "strings"

	config "github.com/0187773933/MastersClosetTracker/server/config"
	hub "github.com/0187773933/MastersClosetTracker/server/hub"
)

const adminUsage = `mct-hub keys create -name <station>    make a station key (printed once)
mct-hub keys list                      every key, live and revoked
mct-hub keys revoke -id <id>           cut a station off (it can be deleted later)
mct-hub keys delete -id <id>           remove a key entirely
mct-hub import -in <file.jsonl.gz>     load an export file (from mct manage export)
mct-hub status                         epoch, change count, records per collection
`

// backend reaches the running hub over its control socket, or opens the
// database directly when the hub is stopped -- the same pattern as the
// station's manage CLI.
func backend() ( selected hub.Backend , err error ) {
	cfg , err := config.Load()
	if err != nil { return }
	if control , dial_err := hub.DialControl( cfg ); dial_err == nil {
		selected = control
	} else {
		store , hub_store , open_err := openHub( cfg )
		if open_err != nil {
			err = open_err
			return
		}
		selected = &hub.DirectBackend{ Hub: hub_store , Release: store.Close }
	}
	fmt.Fprintf( os.Stderr , "[mct-hub] using %s\n" , selected.Route() )
	return
}

func runKeys( arguments []string ) ( err error ) {
	if len( arguments ) == 0 {
		fmt.Print( adminUsage )
		err = fmt.Errorf( "keys needs a command" )
		return
	}
	set := flag.NewFlagSet( "keys "+arguments[ 0 ] , flag.ExitOnError )
	name := set.String( "name" , "" , "which station holds the key" )
	id := set.String( "id" , "" , "key id (from keys list)" )
	set.Parse( arguments[ 1: ] )

	selected , err := backend()
	if err != nil { return }
	defer selected.Close()

	switch arguments[ 0 ] {
	case "create":
		info , credential , create_err := selected.CreateKey( *name )
		if create_err != nil {
			err = create_err
			return
		}
		line := strings.Repeat( "-" , 72 )
		fmt.Println( line )
		fmt.Printf( "  STATION KEY -- %s (id %s)\n" , info.Name , info.ID )
		fmt.Println( line )
		fmt.Printf( "  %s\n" , credential )
		fmt.Println( line )
		fmt.Println( "  Put it in that station's config.yaml as sync.api_key. It is shown" )
		fmt.Println( "  once and cannot be recovered; revoke it and make a new one if lost." )
		fmt.Println( line )
	case "list":
		keys , list_err := selected.ListKeys()
		if list_err != nil {
			err = list_err
			return
		}
		fmt.Printf( "%-14s %-30s %-22s %s\n" , "ID" , "STATION" , "CREATED" , "STATUS" )
		for _ , key := range keys {
			status := "active"
			if key.RevokedAt != nil {
				status = "revoked " + key.RevokedAt.Format( "2006-01-02 15:04" )
			}
			fmt.Printf( "%-14s %-30s %-22s %s\n" , key.ID , key.Name , key.CreatedAt.Local().Format( "2006-01-02 15:04" ) , status )
		}
	case "revoke":
		info , revoke_err := selected.RevokeKey( *id )
		if revoke_err != nil {
			err = revoke_err
			return
		}
		fmt.Printf( "revoked %s (%s)\n" , info.ID , info.Name )
	case "delete":
		if err = selected.DeleteKey( *id ); err != nil { return }
		fmt.Printf( "deleted %s\n" , *id )
	default:
		fmt.Print( adminUsage )
		err = fmt.Errorf( "unknown keys command %q" , arguments[ 0 ] )
	}
	return
}

func runImport( arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "import" , flag.ExitOnError )
	in := set.String( "in" , "" , "export file to load" )
	set.Parse( arguments )
	if *in == "" {
		err = fmt.Errorf( "import needs -in <file>" )
		return
	}
	selected , err := backend()
	if err != nil { return }
	defer selected.Close()
	result , err := selected.Import( *in )
	if err != nil { return }
	fmt.Printf( "read %d records (exported %s from %s): %d stored, %d already current, %d refused\n" ,
		result.Read , result.Header.ExportedAt.Local().Format( "2006-01-02 15:04" ) , result.Header.Source , result.Stored , result.Stale , result.Rejected )
	return
}

func runStatus() ( err error ) {
	selected , err := backend()
	if err != nil { return }
	defer selected.Close()
	status , err := selected.Status()
	if err != nil { return }
	fmt.Printf( "epoch    %s\nchanges  %d\n" , status.Epoch , status.MaxSeq )
	for collection , count := range status.Counts {
		fmt.Printf( "%-8s %d\n" , collection , count )
	}
	return
}
