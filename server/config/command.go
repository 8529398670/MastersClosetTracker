package config

import (
	flag "flag"
	fmt "fmt"
	os "os"
	filepath "path/filepath"
	strconv "strconv"
	strings "strings"
)

const commandUsage = `config -- create, check, or read %[1]s

Usage:
  init  [-file ./%[1]s]        write a new config with fresh keys
                                     (-file - prints it instead)
  example [-file ./%[2]s]
                                     write the committed example: every
                                     default, no keys (-file - prints it)
  check [-file path]                 validate the config in use, say which
                                     file it is, and refuse collisions with
                                     a %[4]s config beside it
  shell [-file path] docker|build    print one section as shell assignments,
                                     for dockerRun.sh and build.sh to eval

Without -file, the file is found the way the server finds it:
  --config <path>, then ./%[1]s, then ~/.config/%[3]s/config.yaml
`

// CommandUsage prints the config subcommands.
func CommandUsage() {
	var others []string
	for _ , sibling := range Siblings() {
		others = append( others , sibling.Binary )
	}
	fmt.Printf( commandUsage , active.LocalFileName , active.ExampleFileName , active.Slug , strings.Join( others , " or " ) )
}

// RunCommand executes `config <subcommand>`. arguments is everything after
// "config".
//
// These exist so that nothing else has to parse YAML. dockerRun.sh and
// build.sh eval the output of `config shell`, which means the scripts get the
// same defaults, the same strict decoding, and the same validation as the
// server -- a typo in the docker section fails the deploy before anything
// starts, instead of being ignored by a second, looser parser.
func RunCommand( arguments []string ) ( err error ) {
	if len( arguments ) < 1 {
		CommandUsage()
		err = fmt.Errorf( "no config command given" )
		return
	}
	command , rest := arguments[ 0 ] , arguments[ 1: ]
	switch command {
	case "init":
		err = commandInit( rest )
	case "example":
		err = commandExample( rest )
	case "check":
		err = commandCheck( rest )
	case "shell":
		err = commandShell( rest )
	default:
		CommandUsage()
		err = fmt.Errorf( "unknown config command %q" , command )
	}
	return
}

func commandInit( arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "config init" , flag.ExitOnError )
	path := set.String( "file" , active.LocalFileName , "where to write it; - for stdout" )
	set.Parse( arguments )

	if *path == "-" {
		var content []byte
		content , err = NewFileContent()
		if err != nil { return }
		_ , err = os.Stdout.Write( content )
		return
	}
	if err = WriteNew( *path ); err != nil { return }
	fmt.Printf( "wrote %s (mode 0600, fresh keys)\n" , *path )
	fmt.Println( "Review it, then keep it out of version control and back it up with the app's data." )
	return
}

// commandExample regenerates config.example.yaml from the embedded defaults,
// so after changing a default the example can be brought back in step with
// one command instead of by hand.
func commandExample( arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "config example" , flag.ExitOnError )
	path := set.String( "file" , active.ExampleFileName , "where to write it; - for stdout" )
	set.Parse( arguments )

	if *path == "-" {
		var content []byte
		content , err = ExampleFileContent()
		if err != nil { return }
		_ , err = os.Stdout.Write( content )
		return
	}
	if err = WriteExample( *path ); err != nil { return }
	fmt.Printf( "wrote %s (no secret key -- safe to commit)\n" , *path )
	return
}

// pickFile resolves -file, or the file the server would use. found is false
// when there is no file, in which case the defaults apply.
func pickFile( explicit string ) ( file *File , path string , found bool , err error ) {
	if explicit == "" {
		file , path , found , err = Current()
		return
	}
	path = explicit
	if isFile( path ) == false {
		file , err = Defaults()
		return
	}
	found = true
	file , err = ReadFile( path )
	return
}

func commandCheck( arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "config check" , flag.ExitOnError )
	explicit := set.String( "file" , "" , "config file to check (default: the one in use)" )
	set.Parse( arguments )

	file , path , found , err := pickFile( *explicit )
	if err != nil { return }
	if found == false {
		if *explicit != "" {
			err = fmt.Errorf( "%s does not exist" , path )
			return
		}
		fmt.Printf( "no config.yaml yet -- the server will create %s on first run\n" , path )
		return
	}
	if active.Stateless == false {
		if err = CheckSecretKey( file , path ); err != nil { return }
	}
	if err = checkSibling( file , path ); err != nil { return }
	fmt.Printf( "ok  %s\n" , path )
	if warning := permissionWarning( path ); warning != "" {
		fmt.Printf( "warning: %s\n" , warning )
	}
	return
}

// checkSibling refuses two configs in the same directory (station, hub,
// site) that would fight over a container name, a published port, or a data
// directory.
// They are usually deployed side by side on one host, and each of those
// collisions fails late and confusingly (the second container will not start,
// or worse, both mount the same bolt file).
func checkSibling( file *File , path string ) ( err error ) {
	for _ , sibling := range Siblings() {
		if err = checkOneSibling( sibling , file , path ); err != nil { return }
	}
	return
}

func checkOneSibling( sibling *Profile , file *File , path string ) ( err error ) {
	sibling_path := filepath.Join( filepath.Dir( path ) , sibling.LocalFileName )
	if isFile( sibling_path ) == false || sibling_path == path {
		return
	}
	raw , err := os.ReadFile( sibling_path )
	if err != nil { return }
	other , err := ParseAs( sibling , raw , sibling_path )
	if err != nil { return }
	var problems []string
	if other.Docker.Container == file.Docker.Container {
		problems = append( problems , fmt.Sprintf( "docker.container %q" , file.Docker.Container ) )
	}
	if other.Docker.HostPort == file.Docker.HostPort && other.Docker.BindAddress == file.Docker.BindAddress {
		problems = append( problems , fmt.Sprintf( "docker.host_port %d" , file.Docker.HostPort ) )
	}
	if filepath.Clean( other.Docker.DataDir ) == filepath.Clean( file.Docker.DataDir ) {
		problems = append( problems , fmt.Sprintf( "docker.data_dir %q" , file.Docker.DataDir ) )
	}
	if other.Docker.Image == file.Docker.Image {
		problems = append( problems , fmt.Sprintf( "docker.image %q" , file.Docker.Image ) )
	}
	if len( problems ) > 0 {
		err = fmt.Errorf( "%s and %s share %s -- give each its own" , path , sibling_path , strings.Join( problems , ", " ) )
	}
	return
}

func commandShell( arguments []string ) ( err error ) {
	set := flag.NewFlagSet( "config shell" , flag.ExitOnError )
	explicit := set.String( "file" , "" , "config file to read (default: the one in use)" )
	set.Parse( arguments )

	if set.NArg() != 1 {
		err = fmt.Errorf( "config shell needs one section: docker or build" )
		return
	}
	file , _ , _ , err := pickFile( *explicit )
	if err != nil { return }

	var lines [][2]string
	switch set.Arg( 0 ) {
	case "docker":
		docker := file.Docker
		// The full -p value is assembled here so the script does not have to
		// know that an IPv6 address needs brackets.
		bind := docker.BindAddress
		if strings.Contains( bind , ":" ) {
			bind = "[" + bind + "]"
		}
		lines = [][2]string{
			{ "docker_image" , docker.Image },
			{ "docker_container" , docker.Container },
			{ "docker_bind_address" , docker.BindAddress },
			{ "docker_host_port" , strconv.Itoa( docker.HostPort ) },
			{ "docker_publish" , fmt.Sprintf( "%s:%d:%d" , bind , docker.HostPort , file.Server.Port ) },
			{ "docker_data_dir" , docker.DataDir },
			{ "docker_restart" , docker.Restart },
			{ "docker_memory" , docker.Memory },
			{ "docker_cpus" , docker.CPUs },
			{ "docker_pids_limit" , strconv.Itoa( docker.PidsLimit ) },
			{ "docker_tmpfs_size" , docker.TmpfsSize },
			{ "server_port" , strconv.Itoa( file.Server.Port ) },
			{ "server_secure_cookies" , strconv.FormatBool( file.Server.SecureCookies ) },
			{ "server_trust_proxy" , strconv.FormatBool( file.Server.TrustProxy ) },
		}
	case "build":
		build := file.Build
		lines = [][2]string{
			{ "build_binary_name" , build.BinaryName },
			{ "build_output_dir" , build.OutputDir },
			{ "build_publish_dir" , build.PublishDir },
			{ "build_targets" , strings.Join( build.Targets , " " ) },
		}
	default:
		err = fmt.Errorf( "unknown section %q: use docker or build" , set.Arg( 0 ) )
		return
	}

	// Never the secret key: nothing a script does needs it, and this output
	// tends to end up in terminal scrollback and CI logs.
	for _ , line := range lines {
		fmt.Printf( "%s=%s\n" , line[ 0 ] , shellQuote( line[ 1 ] ) )
	}
	return
}

// shellQuote single-quotes a value for POSIX sh, which makes any content
// inert under eval -- on top of Validate having already rejected anything
// unusual.
func shellQuote( value string ) ( result string ) {
	result = "'" + strings.ReplaceAll( value , "'" , `'\''` ) + "'"
	return
}
