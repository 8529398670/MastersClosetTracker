package legacy

import (
	errors "errors"

	clients "github.com/0187773933/MastersClosetTracker/server/clients"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
)

// Apply writes converted records into a station, in batches, queuing them for
// the hub. With dry_run it writes nothing and only works out what would
// change -- in particular which families this station does not have yet,
// which is how a second, older database is checked before merging it.
func Apply( store *docs.Store , converted Converted , dry_run bool ) ( report Report , err error ) {
	report = converted.Report
	report.DryRun = dry_run
	report.NewClientIDs = []string{}
	for _ , record := range converted.Records {
		current , get_err := store.Get( record.Collection , record.ID )
		switch {
		case errors.Is( get_err , docs.ErrNotFound ):
			if record.Collection == clients.Collection {
				report.NewClientIDs = append( report.NewClientIDs , record.ID )
			}
		case get_err != nil:
			err = get_err
			return
		case syncproto.Same( current , record ) || syncproto.Wins( record , current ) == false:
			report.AlreadyThere++
		}
	}
	if dry_run {
		report.Applied = len( converted.Records ) - report.AlreadyThere
		return
	}
	for start := 0; start < len( converted.Records ); start += 500 {
		end := min( start+500 , len( converted.Records ) )
		applied , _ , apply_err := store.Import( converted.Records[ start:end ] )
		if apply_err != nil {
			err = apply_err
			return
		}
		report.Applied += applied
	}
	return
}
