package hub

import (
	bytes "bytes"
	context "context"
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	io "io"
	log "log"
	net "net"
	http "net/http"
	os "os"
	runtime "runtime"
	time "time"

	config "github.com/0187773933/MastersClosetTracker/server/config"
)

// The hub's control socket: how `mct-hub keys ...` reaches the database while
// the hub has it open. It follows server/control (the station's) exactly --
// a unix socket in the 0700 app directory, no token, because anything that
// can open it can already read the database and the key beside it.

var ErrNoServer = errors.New( "no running hub" )

const maxControlBytes = 32 << 20 // an import file's worth

type Control struct {
	http *http.Server
	path string
}

type controlError struct {
	Error string `json:"error"`
}

// Backend is what the CLI can do, whether through a running hub or on the
// database directly. ControlBackend and DirectBackend implement it, so the
// two routes cannot drift apart.
type Backend interface {
	ListKeys() ( []KeyInfo , error )
	CreateKey( name string ) ( KeyInfo , string , error )
	RevokeKey( id string ) ( KeyInfo , error )
	DeleteKey( id string ) error
	Import( path string ) ( ImportResult , error )
	Status() ( Status , error )
	Route() string
	Close() error
}

// Status is what `mct-hub status` prints.
type Status struct {
	Epoch  string         `json:"epoch"`
	MaxSeq uint64         `json:"max_seq"`
	Counts map[string]int `json:"counts"`
}

func ( hub *Store ) Status() ( status Status , err error ) {
	status = Status{ Epoch: hub.epoch , MaxSeq: hub.MaxSeq() }
	status.Counts , err = hub.Counts()
	return
}

// ---------------------------------------------------------------------------
// direct: the hub is stopped, the CLI opened the database itself
// ---------------------------------------------------------------------------

type DirectBackend struct {
	Hub     *Store
	Release func() error
}

func ( backend *DirectBackend ) ListKeys() ( keys []KeyInfo , err error ) {
	keys , err = backend.Hub.ListKeys()
	return
}

func ( backend *DirectBackend ) CreateKey( name string ) ( info KeyInfo , credential string , err error ) {
	info , credential , err = backend.Hub.CreateKey( name , time.Now() )
	return
}

func ( backend *DirectBackend ) RevokeKey( id string ) ( info KeyInfo , err error ) {
	info , err = backend.Hub.RevokeKey( id , time.Now() )
	return
}

func ( backend *DirectBackend ) DeleteKey( id string ) ( err error ) {
	err = backend.Hub.DeleteKey( id )
	return
}

func ( backend *DirectBackend ) Import( path string ) ( result ImportResult , err error ) {
	result , err = backend.Hub.ImportFile( path , time.Now() )
	return
}

func ( backend *DirectBackend ) Status() ( status Status , err error ) {
	status , err = backend.Hub.Status()
	return
}

func ( backend *DirectBackend ) Route() ( route string ) {
	route = "database directly (hub not running)"
	return
}

func ( backend *DirectBackend ) Close() ( err error ) {
	err = backend.Release()
	return
}

// ---------------------------------------------------------------------------
// the socket server
// ---------------------------------------------------------------------------

func ListenControl( hub *Store , cfg *config.Config ) ( control *Control , err error ) {
	if cfg.ControlSocketEnabled == false { return }
	path := cfg.ControlSocketPath
	if len( path ) > 103 {
		err = fmt.Errorf( "control socket path is too long for the kernel (%d characters): %s -- set server.app_dir to a shorter path" , len( path ) , path )
		return
	}
	if _ , stat_err := os.Stat( path ); stat_err == nil {
		os.Remove( path )
	}
	listener , err := net.Listen( "unix" , path )
	if err != nil { return }
	if runtime.GOOS != "windows" {
		os.Chmod( path , 0o600 )
	}
	mux := http.NewServeMux()
	reply := func( writer http.ResponseWriter , value any , failure error ) {
		writer.Header().Set( "Content-Type" , "application/json" )
		if failure != nil {
			writer.WriteHeader( http.StatusBadRequest )
			json.NewEncoder( writer ).Encode( controlError{ Error: failure.Error() } )
			return
		}
		json.NewEncoder( writer ).Encode( value )
	}
	decode := func( request *http.Request , out any ) ( err error ) {
		err = json.NewDecoder( io.LimitReader( request.Body , maxControlBytes ) ).Decode( out )
		return
	}
	mux.HandleFunc( "GET /v1/health" , func( writer http.ResponseWriter , _ *http.Request ) {
		reply( writer , map[string]string{ "status": "ok" } , nil )
	} )
	mux.HandleFunc( "GET /v1/status" , func( writer http.ResponseWriter , _ *http.Request ) {
		status , failure := hub.Status()
		reply( writer , status , failure )
	} )
	mux.HandleFunc( "GET /v1/keys" , func( writer http.ResponseWriter , _ *http.Request ) {
		keys , failure := hub.ListKeys()
		reply( writer , keys , failure )
	} )
	mux.HandleFunc( "POST /v1/keys" , func( writer http.ResponseWriter , request *http.Request ) {
		var body struct{ Name string `json:"name"` }
		if failure := decode( request , &body ); failure != nil {
			reply( writer , nil , failure )
			return
		}
		info , credential , failure := hub.CreateKey( body.Name , time.Now() )
		reply( writer , map[string]any{ "key": info , "credential": credential } , failure )
	} )
	mux.HandleFunc( "POST /v1/keys/{id}/revoke" , func( writer http.ResponseWriter , request *http.Request ) {
		info , failure := hub.RevokeKey( request.PathValue( "id" ) , time.Now() )
		reply( writer , info , failure )
	} )
	mux.HandleFunc( "POST /v1/keys/{id}/delete" , func( writer http.ResponseWriter , request *http.Request ) {
		reply( writer , map[string]bool{ "ok": true } , hub.DeleteKey( request.PathValue( "id" ) ) )
	} )
	mux.HandleFunc( "POST /v1/import" , func( writer http.ResponseWriter , request *http.Request ) {
		var body struct{ Path string `json:"path"` }
		if failure := decode( request , &body ); failure != nil {
			reply( writer , nil , failure )
			return
		}
		result , failure := hub.ImportFile( body.Path , time.Now() )
		reply( writer , result , failure )
	} )
	control = &Control{ path: path , http: &http.Server{ Handler: mux , ReadHeaderTimeout: 5 * time.Second } }
	go func() {
		if serve_err := control.http.Serve( listener ); serve_err != nil && errors.Is( serve_err , http.ErrServerClosed ) == false {
			log.Printf( "[control] listener stopped: %v" , serve_err )
		}
	}()
	return
}

func ( control *Control ) Path() ( path string ) {
	if control != nil {
		path = control.path
	}
	return
}

func ( control *Control ) Close() ( err error ) {
	if control == nil { return }
	err = control.http.Close()
	os.Remove( control.path )
	return
}

// ---------------------------------------------------------------------------
// the socket client
// ---------------------------------------------------------------------------

type ControlBackend struct {
	path   string
	client *http.Client
}

// DialControl connects to a running hub, or returns ErrNoServer.
func DialControl( cfg *config.Config ) ( backend *ControlBackend , err error ) {
	if cfg.ControlSocketEnabled == false || len( cfg.ControlSocketPath ) > 103 {
		err = ErrNoServer
		return
	}
	path := cfg.ControlSocketPath
	candidate := &ControlBackend{
		path: path,
		client: &http.Client{
			// An import of a large file takes a while.
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				DialContext: func( ctx context.Context , _ string , _ string ) ( net.Conn , error ) {
					dialer := net.Dialer{}
					return dialer.DialContext( ctx , "unix" , path )
				},
			},
		},
	}
	if candidate.do( "GET" , "/v1/health" , nil , nil ) != nil {
		err = ErrNoServer
		return
	}
	backend = candidate
	return
}

func ( backend *ControlBackend ) do( method string , path string , body any , out any ) ( err error ) {
	var payload io.Reader
	if body != nil {
		encoded , _ := json.Marshal( body )
		payload = bytes.NewReader( encoded )
	}
	request , err := http.NewRequest( method , "http://control"+path , payload )
	if err != nil { return }
	response , err := backend.client.Do( request )
	if err != nil { return }
	defer response.Body.Close()
	raw , err := io.ReadAll( io.LimitReader( response.Body , maxControlBytes ) )
	if err != nil { return }
	if response.StatusCode != http.StatusOK {
		var failure controlError
		if json.Unmarshal( raw , &failure ) == nil && failure.Error != "" {
			err = errors.New( failure.Error )
			return
		}
		err = fmt.Errorf( "hub control returned %s" , response.Status )
		return
	}
	if out != nil {
		err = json.Unmarshal( raw , out )
	}
	return
}

func ( backend *ControlBackend ) ListKeys() ( keys []KeyInfo , err error ) {
	err = backend.do( "GET" , "/v1/keys" , nil , &keys )
	return
}

func ( backend *ControlBackend ) CreateKey( name string ) ( info KeyInfo , credential string , err error ) {
	var out struct {
		Key        KeyInfo `json:"key"`
		Credential string  `json:"credential"`
	}
	err = backend.do( "POST" , "/v1/keys" , map[string]string{ "name": name } , &out )
	info , credential = out.Key , out.Credential
	return
}

func ( backend *ControlBackend ) RevokeKey( id string ) ( info KeyInfo , err error ) {
	err = backend.do( "POST" , "/v1/keys/"+id+"/revoke" , nil , &info )
	return
}

func ( backend *ControlBackend ) DeleteKey( id string ) ( err error ) {
	err = backend.do( "POST" , "/v1/keys/"+id+"/delete" , nil , nil )
	return
}

func ( backend *ControlBackend ) Import( path string ) ( result ImportResult , err error ) {
	err = backend.do( "POST" , "/v1/import" , map[string]string{ "path": path } , &result )
	return
}

func ( backend *ControlBackend ) Status() ( status Status , err error ) {
	err = backend.do( "GET" , "/v1/status" , nil , &status )
	return
}

func ( backend *ControlBackend ) Route() ( route string ) {
	route = "running hub via " + backend.path
	return
}

func ( backend *ControlBackend ) Close() ( err error ) {
	return
}
