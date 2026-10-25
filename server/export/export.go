// Package export writes the check-in history as an Excel workbook: a totals
// sheet, then one sheet per collection day.
package export

import (
	fmt "fmt"
	strings "strings"
	time "time"

	excelize "github.com/xuri/excelize/v2"

	visits "github.com/0187773933/MastersClosetTracker/server/visits"
)

// Day is one collection day's summary and its visits.
type Day struct {
	Summary visits.DaySummary
	Entries []visits.Entry
}

// Label turns a stored day into the sheet's date text.
type Label func( day string ) string

var dayHeaders = []string{ "Name" , "Time" , "Type" , "Shopping For" , "Tops" , "Bottoms" , "Dresses" ,
	"Shoes" , "Seasonals" , "Accessories" , "Boys" , "Girls" , "Men" , "Women" , "Guests" , "Station" , "Visit ID" }

// Workbook builds the .xlsx bytes. location formats each visit's time.
func Workbook( days []Day , label Label , location *time.Location , generated time.Time ) ( content []byte , err error ) {
	file := excelize.NewFile()
	defer file.Close()
	if err = file.SetSheetName( "Sheet1" , "Totals" ); err != nil { return }
	writeTotals( file , "Totals" , days , label , generated.In( location ) )
	used := map[string]bool{ "Totals": true }
	for _ , day := range days {
		name := sheetName( label( day.Summary.Day ) , used )
		if _ , err = file.NewSheet( name ); err != nil { return }
		writeDay( file , name , day.Entries , location )
	}
	file.SetActiveSheet( 0 )
	buffer , err := file.WriteToBuffer()
	if err != nil { return }
	content = buffer.Bytes()
	return
}

func cell( column int , row int ) ( name string ) {
	name , _ = excelize.CoordinatesToCellName( column , row )
	return
}

func writeTotals( file *excelize.File , sheet string , days []Day , label Label , generated time.Time ) {
	file.SetCellValue( sheet , "A1" , "Master's Closet — Check-In Snapshot" )
	file.SetCellValue( sheet , "A2" , "Generated" )
	file.SetCellValue( sheet , "B2" , generated.Format( "2006-01-02 15:04" ) )
	for column , header := range []string{ "Date" , "Check-Ins" , "People Shopped For" , "Manual (People)" , "Total People" } {
		file.SetCellValue( sheet , cell( column+1 , 4 ) , header )
	}
	row , count , shopped , manual := 5 , 0 , 0 , 0
	for _ , day := range days {
		summary := day.Summary
		values := []any{ label( summary.Day ) , summary.Count , summary.ShoppedFor , summary.ManualShoppedFor , summary.ShoppedFor + summary.ManualShoppedFor }
		for column , value := range values {
			file.SetCellValue( sheet , cell( column+1 , row ) , value )
		}
		count += summary.Count
		shopped += summary.ShoppedFor
		manual += summary.ManualShoppedFor
		row++
	}
	for column , value := range []any{ "GRAND TOTAL" , count , shopped , manual , shopped + manual } {
		file.SetCellValue( sheet , cell( column+1 , row ) , value )
	}
}

func writeDay( file *excelize.File , sheet string , entries []visits.Entry , location *time.Location ) {
	for column , header := range dayHeaders {
		file.SetCellValue( sheet , cell( column+1 , 1 ) , header )
	}
	for index , entry := range entries {
		visit := entry.Visit
		kind := visit.Kind
		if visit.LegacyType != "" {
			kind = visit.LegacyType
		}
		values := []any{ visit.Name , visit.At.In( location ).Format( "15:04:05" ) , kind , visit.ShoppingFor ,
			visit.Items[ visits.ItemTops ] , visit.Items[ visits.ItemBottoms ] , visit.Items[ visits.ItemDresses ] ,
			visit.Items[ visits.ItemShoes ] , visit.Items[ visits.ItemSeasonals ] , visit.Items[ visits.ItemAccessories ] ,
			visit.People[ "boys" ] , visit.People[ "girls" ] , visit.People[ "men" ] , visit.People[ "women" ] ,
			visit.Guests , visit.Station , entry.ID }
		for column , value := range values {
			file.SetCellValue( sheet , cell( column+1 , index+2 ) , value )
		}
	}
}

// sheetName makes a label safe and unique as an Excel sheet name (31
// characters, none of : \ / ? * [ ]).
func sheetName( label string , used map[string]bool ) ( name string ) {
	replacer := strings.NewReplacer( ":" , "-" , `\` , "-" , "/" , "-" , "?" , "" , "*" , "" , "[" , "(" , "]" , ")" )
	name = replacer.Replace( label )
	if name == "" {
		name = "Day"
	}
	if len( name ) > 31 {
		name = name[ :31 ]
	}
	base := name
	for suffix := 2; used[ name ]; suffix++ {
		tail := fmt.Sprintf( " (%d)" , suffix )
		if len( base ) > 31-len( tail ) {
			name = base[ :31-len( tail ) ] + tail
		} else {
			name = base + tail
		}
	}
	used[ name ] = true
	return
}
