package manage

import (
	json "encoding/json"
	flag "flag"
	fmt "fmt"
	os "os"
	filepath "path/filepath"
	sort "sort"
	strings "strings"
	time "time"

	config "github.com/0187773933/MastersClosetTracker/server/config"
	legacy "github.com/0187773933/MastersClosetTracker/server/legacy"
)

// The closet's data commands. Each prints counts only -- never a name -- so
// its output is safe to paste anywhere; details go to files you choose.

func absolute( path string ) ( result string , err error ) {
	result , err = filepath.Abs( path )
	return
}

func commandExport( back backend , cfg *config.Config , arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "export" , flag.ExitOnError )
	out := set.String( "out" , "" , "file to write (default: <app dir>/exports/export-<time>.jsonl.gz)" )
	set.Parse( arguments )
	path := *out
	if path == "" {
		path = filepath.Join( cfg.AppDir , "exports" , "export-"+time.Now().Format( "20060102-150405" )+".jsonl.gz" )
	}
	if path , err = absolute( path ); err != nil { return }
	result , err := back.Export( path )
	if err != nil { return }
	fmt.Printf( "wrote %d records to %s\n" , result.Records , result.Path )
	for _ , collection := range []string{ "clients" , "visits" , "day_tallies" , "settings" } {
		fmt.Printf( "  %-12s %d\n" , collection , result.Counts[ collection ] )
	}
	fmt.Println( "It holds every family's details: keep it somewhere only you can read." )
	return
}

func commandImport( back backend , arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "import" , flag.ExitOnError )
	in := set.String( "in" , "" , "export file to load" )
	set.Parse( arguments )
	if *in == "" {
		err = fmt.Errorf( "import needs -in <file>" )
		return
	}
	path , err := absolute( *in )
	if err != nil { return }
	result , err := back.Import( path )
	if err != nil { return }
	fmt.Printf( "read %d records (exported %s from database %s): %d applied, %d already current or invalid\n" ,
		result.Read , result.Header.ExportedAt.Local().Format( "2006-01-02 15:04" ) , result.Header.Source , result.Applied , result.Read-result.Applied )
	return
}

// oldKey reads the old system's encryption key from its config.json (or a
// file holding just the key), so it never has to be typed on a command line.
func oldKey( config_path string , key_path string ) ( key string , err error ) {
	switch {
	case key_path != "":
		var raw []byte
		if raw , err = os.ReadFile( key_path ); err != nil { return }
		key = strings.TrimSpace( string( raw ) )
	case config_path != "":
		var raw []byte
		if raw , err = os.ReadFile( config_path ); err != nil { return }
		var old struct {
			Key string `json:"bolt_db_encryption_key"`
		}
		if err = json.Unmarshal( raw , &old ); err != nil {
			err = fmt.Errorf( "%s is not the old config.json: %w" , config_path , err )
			return
		}
		key = strings.TrimSpace( old.Key )
	}
	if len( key ) != 64 {
		err = fmt.Errorf( "need the old bolt_db_encryption_key: pass -legacy-config <old config.json> or -key-file <file>" )
	}
	return
}

func commandImportLegacy( back backend , arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "import-legacy" , flag.ExitOnError )
	database := set.String( "db" , "" , "the old bolt database (opened read-only)" )
	legacy_config := set.String( "legacy-config" , "" , "the old config.json (for its encryption key)" )
	key_file := set.String( "key-file" , "" , "a file holding just the old encryption key" )
	with_settings := set.Bool( "settings" , false , "also import allowances, cooloff, time zone, ..." )
	settings_json := set.String( "settings-json" , "" , "read those settings from this old config.json instead of the database" )
	time_zone := set.String( "time-zone" , "America/New_York" , "the zone the old dates were written in" )
	dry_run := set.Bool( "dry-run" , false , "change nothing; report what would be imported" )
	report_path := set.String( "report" , "" , "write the full report (with the ids of families new to this station) here" )
	set.Parse( arguments )
	if *database == "" {
		err = fmt.Errorf( "import-legacy needs -db <old database>" )
		return
	}
	key , err := oldKey( *legacy_config , *key_file )
	if err != nil { return }
	options := legacy.Options{ Key: key , Settings: *with_settings , TimeZone: *time_zone }
	if options.DBPath , err = absolute( *database ); err != nil { return }
	if *settings_json != "" {
		if options.SettingsJSON , err = absolute( *settings_json ); err != nil { return }
	}

	report , err := back.ImportLegacy( options , *dry_run )
	if err != nil { return }

	mode := "imported"
	if report.DryRun {
		mode = "would import (dry run -- nothing written)"
	}
	fmt.Printf( "%s from %s\n" , mode , report.Source )
	fmt.Printf( "  old user records   %d (%d unreadable)\n" , report.Users , len( report.Problems ) )
	fmt.Printf( "  clients            %d (%d new to this station, %d given a virtual barcode)\n" , report.Clients , len( report.NewClientIDs ) , report.DerivedBarcode )
	types := make( []string , 0 , len( report.VisitsByType ) )
	for kind , count := range report.VisitsByType {
		types = append( types , fmt.Sprintf( "%s %d" , kind , count ) )
	}
	sort.Strings( types )
	fmt.Printf( "  visits             %d (%s), %d without a ULID\n" , report.Visits , strings.Join( types , ", " ) , report.VisitsNoULID )
	fmt.Printf( "  refused attempts   %d\n" , report.Refused )
	fmt.Printf( "  day tallies        %d\n" , report.Tallies )
	fmt.Printf( "  settings           %v\n" , report.Settings )
	fmt.Printf( "  records applied    %d (%d already here)\n" , report.Applied , report.AlreadyThere )
	fmt.Printf( "  barcodes shared by more than one family: %d\n" , len( report.SharedBarcodes ) )
	fmt.Printf( "  families with two visits on one day:      %d (see a day's page: Delete duplicates)\n" , report.SameDayRepeats )
	for _ , problem := range report.Problems {
		fmt.Printf( "  ! %s: %s\n" , problem.Key , problem.Reason )
	}
	if *report_path != "" {
		encoded , _ := json.MarshalIndent( report , "" , "  " )
		if err = os.WriteFile( *report_path , encoded , 0o600 ); err != nil { return }
		fmt.Printf( "full report: %s\n" , *report_path )
	}
	return
}
