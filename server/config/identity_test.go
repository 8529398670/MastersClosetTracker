package config

import (
	errors "errors"
	strings "strings"
	testing "testing"
)

func standIn( t *testing.T , host string , login string , machine string ) {
	t.Helper()
	old_host , old_login , old_machine := readHostname , readUsername , readMachineID
	t.Cleanup( func() { readHostname , readUsername , readMachineID = old_host , old_login , old_machine } )
	fail := errors.New( "unavailable" )
	readHostname = func() ( string , error ) { if host == "" { return "" , fail } ; return host , nil }
	readUsername = func() ( string , error ) { if login == "" { return "" , fail } ; return login , nil }
	readMachineID = func() ( string , error ) { if machine == "" { return "" , fail } ; return machine , nil }
}

// Two logins on one PC, and two PCs with one name, all come out different.
func TestAutoStationNameTellsStationsApart( t *testing.T ) {
	standIn( t , "FRONTDESK-PC" , `CLOSET\jane` , "3F1C0000-1111-2222-3333-444455556666" )
	jane := AutoStationName( "abcdef0123456789" )
	if strings.HasPrefix( jane , "FRONTDESK-PC / jane (" ) == false || len( jane ) != len( "FRONTDESK-PC / jane (123456)" ) {
		t.Fatalf( "name = %q" , jane )
	}
	standIn( t , "FRONTDESK-PC" , `CLOSET\sam` , "3F1C0000-1111-2222-3333-444455556666" )
	if sam := AutoStationName( "abcdef0123456789" ); sam == jane {
		t.Fatalf( "two logins share %q" , sam )
	}
	standIn( t , "FRONTDESK-PC" , `CLOSET\jane` , "99990000-1111-2222-3333-444455556666" )
	if other := AutoStationName( "abcdef0123456789" ); other == jane {
		t.Fatalf( "two machines share %q" , other )
	}
}

// With no hardware id the database id stands in, and .local is dropped.
func TestAutoStationNameFallsBack( t *testing.T ) {
	standIn( t , "laptop.local" , "jane" , "" )
	if name := AutoStationName( "abcdef0123456789" ); name != "laptop / jane (abcdef)" {
		t.Fatalf( "name = %q" , name )
	}
	standIn( t , "" , "" , "" )
	if name := AutoStationName( "abcdef0123456789" ); name != "station abcdef" {
		t.Fatalf( "name = %q" , name )
	}
}

// The real machine answers something on the OS running the tests.
func TestAutoStationNameOnThisMachine( t *testing.T ) {
	if name := AutoStationName( "abcdef" ); name == "" {
		t.Fatal( "empty name" )
	} else {
		t.Logf( "this machine: %q" , name )
	}
}
