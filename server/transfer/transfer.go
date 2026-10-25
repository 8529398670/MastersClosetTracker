// Package transfer is the export file: every record a station or hub holds,
// as gzip-compressed JSON lines. The first line is a header; every line after
// it is one envelope exactly as it syncs (see server/syncproto), stamp and
// all.
//
// Because records keep their stamps, importing a file is the same as
// receiving those records from the hub: a record is replaced only by a
// version that wins, so importing a file twice changes nothing, and
// importing an old backup can never undo an edit made since. The same file
// seeds a fresh hub (`mct-hub import`), restores a station, or moves data
// between machines.
package transfer

import (
	bufio "bufio"
	gzip "compress/gzip"
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	io "io"
	os "os"
	time "time"

	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
)

const (
	Format  = "masters-closet-export"
	Version = 1

	// maxLine bounds one record's line; the hub refuses records far smaller.
	maxLine = 8 << 20
)

type Header struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	ExportedAt time.Time      `json:"exported_at"`
	Source     string         `json:"source"`
	Counts     map[string]int `json:"counts"`
}

// Writer streams an export.
type Writer struct {
	file    io.Writer
	gzip    *gzip.Writer
	encoder *json.Encoder
}

func NewWriter( out io.Writer , header Header ) ( writer *Writer , err error ) {
	header.Format , header.Version = Format , Version
	compressed := gzip.NewWriter( out )
	writer = &Writer{ file: out , gzip: compressed , encoder: json.NewEncoder( compressed ) }
	err = writer.encoder.Encode( header )
	return
}

func ( writer *Writer ) Write( envelope syncproto.Envelope ) ( err error ) {
	err = writer.encoder.Encode( envelope )
	return
}

func ( writer *Writer ) Close() ( err error ) {
	err = writer.gzip.Close()
	return
}

// Read walks a file, handing over envelopes in batches of batch_size.
func Read( in io.Reader , batch_size int , visit func( batch []syncproto.Envelope ) error ) ( header Header , total int , err error ) {
	decompressed , err := gzip.NewReader( in )
	if err != nil {
		err = fmt.Errorf( "not an export file (expected gzip): %w" , err )
		return
	}
	defer decompressed.Close()
	scanner := bufio.NewScanner( decompressed )
	scanner.Buffer( make( []byte , 64*1024 ) , maxLine )
	if scanner.Scan() == false {
		err = errors.New( "export file is empty" )
		return
	}
	if err = json.Unmarshal( scanner.Bytes() , &header ); err != nil || header.Format != Format {
		err = errors.New( "not a Master's Closet export file" )
		return
	}
	if header.Version > Version {
		err = fmt.Errorf( "export file is version %d; this build reads up to %d -- upgrade first" , header.Version , Version )
		return
	}
	batch := make( []syncproto.Envelope , 0 , batch_size )
	line := 1
	for scanner.Scan() {
		line++
		var envelope syncproto.Envelope
		if err = json.Unmarshal( scanner.Bytes() , &envelope ); err != nil {
			err = fmt.Errorf( "line %d: %w" , line , err )
			return
		}
		batch = append( batch , envelope )
		total++
		if len( batch ) == batch_size {
			if err = visit( batch ); err != nil { return }
			batch = batch[ :0 ]
		}
	}
	if err = scanner.Err(); err != nil { return }
	if len( batch ) > 0 {
		err = visit( batch )
	}
	return
}

// ReadFile is Read on a path.
func ReadFile( path string , batch_size int , visit func( batch []syncproto.Envelope ) error ) ( header Header , total int , err error ) {
	file , err := os.Open( path )
	if err != nil { return }
	defer file.Close()
	header , total , err = Read( file , batch_size , visit )
	return
}
