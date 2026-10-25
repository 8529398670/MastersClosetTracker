package settings

import (
	errors "errors"
	fmt "fmt"
	filepath "path/filepath"
	testing "testing"
	time "time"

	db "github.com/0187773933/MastersClosetTracker/server/db"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	testutil "github.com/0187773933/MastersClosetTracker/server/testutil"
)

func openService( t *testing.T , path string ) ( *Service , *docs.Store , *db.Store ) {
	t.Helper()
	store := testutil.OpenStore( t , path , append( append( []string{} , docs.Buckets... ) , Buckets... )... )
	docs_store , err := docs.Open( store , func() time.Time { return time.UnixMilli( 1_700_000_000_000 ) } )
	if err != nil {
		t.Fatal( err )
	}
	cfg := testutil.Config( t )
	service , err := New( docs_store , store , cfg )
	if err != nil {
		t.Fatal( err )
	}
	return service , docs_store , store
}

// pageEdits is what the Settings page sends: every editable field, as text.
func pageEdits( service *Service ) ( edits map[string]any ) {
	edits = map[string]any{}
	for path , value := range service.Values() {
		edits[ path ] = fmt.Sprint( value )
	}
	return
}

func TestDefaultsWithNoRecords( t *testing.T ) {
	service , _ , store := openService( t , filepath.Join( t.TempDir() , "a.db" ) )
	defer store.Close()
	org := service.Org()
	if org.CooloffDays != 30 || org.Balance.General.Total != 6 || org.Balance.Accessories != 2 || org.AutoVerifyZipcode != "45424" {
		t.Fatalf( "unexpected defaults: %+v" , org )
	}
	if org.Location.String() != "America/New_York" {
		t.Fatalf( "time zone default: %v" , org.Location )
	}
	if service.Printer().Speed != 2 || service.Printer().PageWidth != 4 {
		t.Fatalf( "printer defaults: %+v" , service.Printer() )
	}
}

func TestSavingThePageUntouchedWritesNothing( t *testing.T ) {
	service , docs_store , store := openService( t , filepath.Join( t.TempDir() , "a.db" ) )
	defer store.Close()
	if _ , err := service.Update( pageEdits( service ) ); err != nil {
		t.Fatal( err )
	}
	live , _ , _ := docs_store.Counts()
	if live[ Collection ] != 0 || docs_store.PendingCount() != 0 {
		t.Fatalf( "an untouched save wrote %d records" , live[ Collection ] )
	}
}

func TestOneChangeWritesOneRecordAndNodeSettingsStayLocal( t *testing.T ) {
	service , docs_store , store := openService( t , filepath.Join( t.TempDir() , "a.db" ) )
	defer store.Close()
	edits := pageEdits( service )
	edits[ "check_in_cooloff_days" ] = "21"
	edits[ "printer.printer_name" ] = "_4BARCODE_4B_2054N"
	if _ , err := service.Update( edits ); err != nil {
		t.Fatal( err )
	}
	live , _ , _ := docs_store.Counts()
	if live[ Collection ] != 1 {
		t.Fatalf( "want exactly one synced record, got %d" , live[ Collection ] )
	}
	if service.Org().CooloffDays != 21 || service.Printer().Name != "_4BARCODE_4B_2054N" {
		t.Fatalf( "values not in effect: %+v %+v" , service.Org() , service.Printer() )
	}
}

func TestOneBadValueSavesNothing( t *testing.T ) {
	service , _ , store := openService( t , filepath.Join( t.TempDir() , "a.db" ) )
	defer store.Close()
	edits := pageEdits( service )
	edits[ "check_in_cooloff_days" ] = "14"
	edits[ "time_zone" ] = "Mars/Olympus"
	_ , err := service.Update( edits )
	var field_error *FieldError
	if errors.As( err , &field_error ) == false || field_error.Path != "time_zone" {
		t.Fatalf( "want a time_zone FieldError, got %v" , err )
	}
	if service.Org().CooloffDays != 30 {
		t.Fatalf( "a refused save must not apply the other fields" )
	}
	edits = pageEdits( service )
	edits[ "max_family_size" ] = "2.5"
	if _ , err := service.Update( edits ); err == nil {
		t.Fatalf( "a fractional family size should be refused" )
	}
}

func TestRemoteSettingIsAbsorbedAndPersists( t *testing.T ) {
	dir := t.TempDir()
	a , a_docs , a_store := openService( t , filepath.Join( dir , "a.db" ) )
	defer a_store.Close()
	b , b_docs , b_store := openService( t , filepath.Join( dir , "b.db" ) )

	edits := pageEdits( a )
	edits[ "balance.shoes" ] = "3"
	a.Update( edits )
	edits = pageEdits( b )
	edits[ "auto_verify_zipcode" ] = "45431"
	b.Update( edits )

	// exchange both ways
	from_a , _ := a_docs.Pending( 100 , 1<<20 )
	from_b , _ := b_docs.Pending( 100 , 1<<20 )
	b_docs.Apply( from_a )
	a_docs.Apply( from_b )
	for _ , service := range []*Service{ a , b } {
		org := service.Org()
		if org.Balance.Shoes != 3 || org.AutoVerifyZipcode != "45431" {
			t.Fatalf( "per-setting merge lost a change: %+v" , org )
		}
	}

	// b restarts and still has both
	b_store.Close()
	b , _ , b_store = openService( t , filepath.Join( dir , "b.db" ) )
	defer b_store.Close()
	if b.Org().Balance.Shoes != 3 {
		t.Fatalf( "synced setting lost on restart" )
	}
}

// The station's own setting beats config.yaml, which beats the machine.
func TestStationNamePrecedence( t *testing.T ) {
	service , _ , store := openService( t , filepath.Join( t.TempDir() , "a.db" ) )
	defer store.Close()
	auto := service.StationName()
	if auto == "" {
		t.Fatal( "no automatic name" )
	}
	service.cfg.SyncNodeName = "Shared Name"
	if name := service.StationName(); name != "Shared Name" {
		t.Fatalf( "config name ignored: %q" , name )
	}
	edits := pageEdits( service )
	edits[ "station.name" ] = "  Front Desk  "
	restart , err := service.Update( edits )
	if err != nil {
		t.Fatal( err )
	}
	if len( restart ) != 1 || restart[ 0 ] != "Station Name" {
		t.Fatalf( "restart_required = %v" , restart )
	}
	if name := service.StationName(); name != "Front Desk" {
		t.Fatalf( "station setting ignored: %q" , name )
	}
	if service.Values()[ "station.node_name" ] != "Front Desk" {
		t.Fatalf( "Settings page shows %v" , service.Values()[ "station.node_name" ] )
	}
	edits[ "station.name" ] = ""
	service.cfg.SyncNodeName = ""
	if _ , err = service.Update( edits ); err != nil {
		t.Fatal( err )
	}
	if name := service.StationName(); name != auto {
		t.Fatalf( "clearing it gave %q, want %q" , name , auto )
	}
}
