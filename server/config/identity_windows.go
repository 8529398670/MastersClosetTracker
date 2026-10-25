//go:build windows

package config

import (
	context "context"
	errors "errors"
	exec "os/exec"
	strings "strings"
	time "time"
)

// machineID is the MachineGuid Windows writes at install time. reg.exe
// rather than a registry package keeps the dependency list as it is.
func machineID() ( id string , err error ) {
	ctx , cancel := context.WithTimeout( context.Background() , 3*time.Second )
	defer cancel()
	out , err := exec.CommandContext( ctx , "reg" , "query" , `HKLM\SOFTWARE\Microsoft\Cryptography` , "/v" , "MachineGuid" ).Output()
	if err != nil { return }
	// "    MachineGuid    REG_SZ    3f1c...-..."
	for _ , line := range strings.Split( string( out ) , "\n" ) {
		fields := strings.Fields( line )
		if len( fields ) == 3 && strings.EqualFold( fields[ 0 ] , "MachineGuid" ) {
			id = fields[ 2 ]
			return
		}
	}
	err = errors.New( "MachineGuid not found" )
	return
}
