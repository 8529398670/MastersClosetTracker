package config

import (
	sha256 "crypto/sha256"
	hex "encoding/hex"
	os "os"
	user "os/user"
	strings "strings"
)

// Every station runs from one shared folder (Dropbox), so they all read the
// same config.yaml and nothing in it can tell them apart. A station's name is
// worked out on the machine instead: computer name, login, and a short hash
// of the OS machine id, e.g. "FRONTDESK-PC / jane (3fa9c1)". The login is
// there because two people on one PC each get their own database; the hash
// is there because two PCs can share a name. The raw machine id is never
// shown -- only enough of its hash to tell stations apart.

// These are variables so tests can stand in for the machine.
var (
	readHostname  = os.Hostname
	readUsername  = func() ( name string , err error ) {
		current , err := user.Current()
		if err == nil { name = current.Username }
		return
	}
	readMachineID = machineID
)

// AutoStationName is this machine's station name. fallback_id stands in for
// the hardware id when the OS will not give one up -- the database id is
// unique per install, which is all the suffix is for. Each part that cannot
// be read is left out rather than failing.
func AutoStationName( fallback_id string ) ( name string ) {
	var parts []string
	if host , err := readHostname(); err == nil {
		host = strings.TrimSpace( host )
		host = strings.TrimSuffix( host , ".local" )
		if host != "" { parts = append( parts , host ) }
	}
	if login , err := readUsername(); err == nil {
		// Windows answers DOMAIN\jane.
		if index := strings.LastIndex( login , `\` ); index >= 0 {
			login = login[ index+1: ]
		}
		if login = strings.TrimSpace( login ); login != "" {
			parts = append( parts , login )
		}
	}
	name = strings.Join( parts , " / " )

	suffix := ""
	if id , err := readMachineID(); err == nil && strings.TrimSpace( id ) != "" {
		sum := sha256.Sum256( []byte( strings.ToLower( strings.TrimSpace( id ) ) ) )
		suffix = hex.EncodeToString( sum[:] )[ :6 ]
	} else if len( fallback_id ) >= 6 {
		suffix = fallback_id[ :6 ]
	} else {
		suffix = fallback_id
	}
	switch {
	case name == "" && suffix == "":
		name = "station"
	case name == "":
		name = "station " + suffix
	case suffix != "":
		name += " (" + suffix + ")"
	}
	return
}
