package control

import (
	bytes "bytes"
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	io "io"
	http "net/http"
	time "time"

	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	legacy "github.com/0187773933/MastersClosetTracker/server/legacy"
	transfer "github.com/0187773933/MastersClosetTracker/server/transfer"
)

// The closet's data operations -- export, import, and importing the old
// system's database -- over the same socket as the account commands. They
// run inside the server so imported records reach the in-memory index and
// the sync outbox the moment they are written.

// maxDataResponseBytes allows for an import report listing every new family.
const maxDataResponseBytes = 16 << 20

// SetData gives the control server the closet's document store. Without it
// (or with no control server at all) the data endpoints answer 503.
func ( server *Server ) SetData( store *docs.Store ) {
	if server == nil { return }
	server.data = store
}

type DataPathRequest struct {
	Path string `json:"path"`
}

type LegacyRequest struct {
	Options legacy.Options `json:"options"`
	DryRun  bool           `json:"dry_run"`
}

func ( server *Server ) dataReady( w http.ResponseWriter ) ( ok bool ) {
	if server.data == nil {
		writeError( w , http.StatusServiceUnavailable , "this server has no data store" )
		return
	}
	ok = true
	return
}

func ( server *Server ) handleExport( w http.ResponseWriter , r *http.Request ) {
	var body DataPathRequest
	if decodeBody( w , r , &body ) == false || server.dataReady( w ) == false { return }
	result , err := transfer.Export( server.data , body.Path , time.Now() )
	if err != nil {
		writeError( w , http.StatusBadRequest , err.Error() )
		return
	}
	writeJSON( w , http.StatusOK , result )
}

func ( server *Server ) handleImport( w http.ResponseWriter , r *http.Request ) {
	var body DataPathRequest
	if decodeBody( w , r , &body ) == false || server.dataReady( w ) == false { return }
	result , err := transfer.Import( server.data , body.Path )
	if err != nil {
		writeError( w , http.StatusBadRequest , err.Error() )
		return
	}
	writeJSON( w , http.StatusOK , result )
}

func ( server *Server ) handleImportLegacy( w http.ResponseWriter , r *http.Request ) {
	var body LegacyRequest
	if decodeBody( w , r , &body ) == false || server.dataReady( w ) == false { return }
	report , err := ImportLegacy( server.data , body.Options , body.DryRun )
	if err != nil {
		writeError( w , http.StatusBadRequest , err.Error() )
		return
	}
	writeJSON( w , http.StatusOK , report )
}

// ImportLegacy reads an old database and applies it (or checks it, in a dry
// run). The direct route in server/manage calls this too.
func ImportLegacy( store *docs.Store , options legacy.Options , dry_run bool ) ( report legacy.Report , err error ) {
	converted , err := legacy.Read( options )
	if err != nil { return }
	report , err = legacy.Apply( store , converted , dry_run )
	return
}

// slow is do for the data operations: they can run for minutes and answer
// with more than an account command does.
func ( client *Client ) slow( path string , body any , out any ) ( err error ) {
	encoded , err := json.Marshal( body )
	if err != nil { return }
	request , err := http.NewRequest( "POST" , "http://control"+path , bytes.NewReader( encoded ) )
	if err != nil { return }
	request.Header.Set( "Content-Type" , "application/json" )
	patient := *client.http
	patient.Timeout = 30 * time.Minute
	response , err := patient.Do( request )
	if err != nil { return }
	defer response.Body.Close()
	raw , err := io.ReadAll( io.LimitReader( response.Body , maxDataResponseBytes ) )
	if err != nil { return }
	if response.StatusCode < 200 || response.StatusCode > 299 {
		failure := errorBody{}
		if json.Unmarshal( raw , &failure ) == nil && failure.Error != "" {
			err = errors.New( failure.Error )
			return
		}
		err = fmt.Errorf( "control server returned %s" , response.Status )
		return
	}
	err = json.Unmarshal( raw , out )
	return
}

func ( client *Client ) Export( path string ) ( result transfer.ExportResult , err error ) {
	err = client.slow( "/v1/data/export" , DataPathRequest{ Path: path } , &result )
	return
}

func ( client *Client ) Import( path string ) ( result transfer.ImportResult , err error ) {
	err = client.slow( "/v1/data/import" , DataPathRequest{ Path: path } , &result )
	return
}

func ( client *Client ) ImportLegacy( options legacy.Options , dry_run bool ) ( report legacy.Report , err error ) {
	err = client.slow( "/v1/data/import-legacy" , LegacyRequest{ Options: options , DryRun: dry_run } , &report )
	return
}
