package export

import (
	bytes "bytes"
	testing "testing"
	time "time"

	excelize "github.com/xuri/excelize/v2"

	closet "github.com/0187773933/MastersClosetTracker/server/closet"
)

func TestBarcodesWorkbook( t *testing.T ) {
	at := time.Date( 2026 , 6 , 6 , 15 , 4 , 0 , 0 , time.UTC )
	rows := []closet.BarcodeRow{
		{ Barcode: "0099" , ClientID: "a" , Name: "Ana Lopez" , AssignedAt: at , Owners: 2 },
		{ Barcode: "0099" , ClientID: "b" , Name: "Bo Kim" , Estimated: true , Owners: 2 },
	}
	content , err := Barcodes( rows , time.UTC , at )
	if err != nil {
		t.Fatal( err )
	}
	file , err := excelize.OpenReader( bytes.NewReader( content ) )
	if err != nil {
		t.Fatal( err )
	}
	defer file.Close()
	got , _ := file.GetRows( "Barcodes" )
	if len( got ) != 3 || got[ 1 ][ 0 ] != "0099" || got[ 1 ][ 2 ] != "2026-06-06 15:04" || got[ 2 ][ 2 ] != "" || got[ 2 ][ 3 ] != "yes" || got[ 2 ][ 4 ] != "2" {
		t.Fatalf( "rows: %q" , got )
	}
	if style , _ := file.GetCellStyle( "Barcodes" , "A2" ); style == 0 {
		t.Fatalf( "a shared barcode should be highlighted" )
	}
}
