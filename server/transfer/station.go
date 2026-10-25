package transfer

import (
	fmt "fmt"
	os "os"
	filepath "path/filepath"
	time "time"

	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
)

// Collections is every collection an export carries, in the order written.
var Collections = []string{ "clients" , "visits" , "day_tallies" , "settings" }

type ExportResult struct {
	Path    string         `json:"path"`
	Records int            `json:"records"`
	Counts  map[string]int `json:"counts"`
}

// Export writes every record a station holds, tombstones included, to path
// (mode 0600: it holds every family's details).
func Export( store *docs.Store , path string , now time.Time ) ( result ExportResult , err error ) {
	result = ExportResult{ Path: path , Counts: map[string]int{} }
	if err = os.MkdirAll( filepath.Dir( path ) , 0o700 ); err != nil { return }
	file , err := os.OpenFile( path , os.O_WRONLY|os.O_CREATE|os.O_EXCL , 0o600 )
	if err != nil {
		err = fmt.Errorf( "could not create %s (it must not exist yet): %w" , path , err )
		return
	}
	defer file.Close()
	live , deleted , err := store.Counts()
	if err != nil { return }
	counts := map[string]int{}
	for _ , collection := range Collections {
		counts[ collection ] = live[ collection ] + deleted[ collection ]
	}
	writer , err := NewWriter( file , Header{ ExportedAt: now.UTC() , Source: store.Node() , Counts: counts } )
	if err != nil { return }
	for _ , collection := range Collections {
		var write_err error
		err = store.ForEach( collection , func( envelope syncproto.Envelope ) bool {
			if write_err = writer.Write( envelope ); write_err != nil {
				return false
			}
			result.Counts[ collection ]++
			result.Records++
			return true
		} )
		if err == nil {
			err = write_err
		}
		if err != nil { return }
	}
	if err = writer.Close(); err != nil { return }
	err = file.Sync()
	return
}

type ImportResult struct {
	Header  Header `json:"header"`
	Read    int    `json:"read"`
	Applied int    `json:"applied"`
	Skipped int    `json:"skipped"`
}

// Import loads an export file into a station: records that win are applied
// and queued for the hub, the rest are already there.
func Import( store *docs.Store , path string ) ( result ImportResult , err error ) {
	result.Header , result.Read , err = ReadFile( path , 500 , func( batch []syncproto.Envelope ) ( batch_err error ) {
		applied , skipped , batch_err := store.Import( batch )
		result.Applied += applied
		result.Skipped += skipped
		return
	} )
	return
}
