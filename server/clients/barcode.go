package clients

import (
	binary "encoding/binary"
	fmt "fmt"
	time "time"

	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
)

// Virtual barcodes are minted for every client who has no card of their
// own, so every ticket can carry one. They are ten digits starting with 9:
// random rather than counted, because a counter per station is exactly what
// gave two families the same number in the old system. The station re-rolls
// on the (one in a billion) chance of a collision it can see.
const virtualSpace = 1_000_000_000

// MintBarcode returns a fresh virtual barcode for which taken reports false.
func MintBarcode( taken func( barcode string ) bool ) ( barcode string , err error ) {
	for attempt := 0; attempt < 100; attempt++ {
		// A 64-bit draw folded into a billion: the bias is ~1 in 10^10.
		barcode = fmt.Sprintf( "9%09d" , encryption.RandomUint64()%virtualSpace )
		if taken( barcode ) == false { return }
	}
	err = fmt.Errorf( "could not find a free barcode" )
	return
}

// DerivedBarcode is a virtual barcode computed from a client id, for
// imported records that never had one: the same record always gets the same
// barcode, so importing twice does not mint two.
func DerivedBarcode( client_id string , taken func( barcode string ) bool ) ( barcode string ) {
	for salt := 0; ; salt++ {
		sum := encryption.Sha256Sum( [][]byte{ []byte( fmt.Sprintf( "%s/%d" , client_id , salt ) ) } )
		barcode = fmt.Sprintf( "9%09d" , binary.BigEndian.Uint64( sum[ :8 ] )%virtualSpace )
		if taken( barcode ) == false { return }
	}
}

// StampBarcodes records when each of a client's barcodes was assigned:
// barcodes the previous version of the record (nil for a new one) already
// had keep their date -- or stay undated, if they predate the dates being
// recorded -- new ones get now, and removed ones are dropped. Whatever dates
// the page sent are ignored: the server decides when a barcode was assigned.
func StampBarcodes( client *Client , previous *Client , now time.Time ) {
	had := map[string]bool{}
	var dates map[string]time.Time
	if previous != nil {
		for _ , barcode := range previous.Barcodes {
			had[ barcode ] = true
		}
		dates = previous.BarcodeAssigned
	}
	assigned := map[string]time.Time{}
	for _ , barcode := range client.Barcodes {
		if at , ok := dates[ barcode ]; ok {
			assigned[ barcode ] = at
		} else if had[ barcode ] == false {
			assigned[ barcode ] = now
		}
	}
	client.BarcodeAssigned = assigned
}
