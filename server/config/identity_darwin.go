//go:build darwin

package config

import (
	context "context"
	errors "errors"
	exec "os/exec"
	strings "strings"
	time "time"
)

// machineID is the hardware UUID (System Information shows the same one).
func machineID() ( id string , err error ) {
	ctx , cancel := context.WithTimeout( context.Background() , 3*time.Second )
	defer cancel()
	out , err := exec.CommandContext( ctx , "ioreg" , "-rd1" , "-c" , "IOPlatformExpertDevice" ).Output()
	if err != nil { return }
	// "IOPlatformUUID" = "3F1C...-..."
	for _ , line := range strings.Split( string( out ) , "\n" ) {
		if strings.Contains( line , `"IOPlatformUUID"` ) == false { continue }
		if _ , value , ok := strings.Cut( line , "=" ); ok {
			id = strings.Trim( strings.TrimSpace( value ) , `"` )
			return
		}
	}
	err = errors.New( "IOPlatformUUID not found" )
	return
}
