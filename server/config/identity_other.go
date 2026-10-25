//go:build !windows && !darwin

package config

import (
	os "os"
	strings "strings"
)

// machineID is systemd's machine id, or D-Bus's copy of it.
func machineID() ( id string , err error ) {
	for _ , path := range []string{ "/etc/machine-id" , "/var/lib/dbus/machine-id" } {
		var raw []byte
		if raw , err = os.ReadFile( path ); err == nil {
			id = strings.TrimSpace( string( raw ) )
			if id != "" { return }
		}
	}
	if err == nil { err = os.ErrNotExist }
	return
}
