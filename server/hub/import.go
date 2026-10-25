package hub

import (
	time "time"

	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
	transfer "github.com/0187773933/MastersClosetTracker/server/transfer"
)

// ImportResult is what loading an export file into the hub did.
type ImportResult struct {
	Header   transfer.Header `json:"header"`
	Read     int             `json:"read"`
	Stored   int             `json:"stored"`
	Stale    int             `json:"stale"`
	Rejected int             `json:"rejected"`
}

// ImportFile loads an export file the same way stations' pushes are taken:
// a record is stored only if it wins over what the hub has. A fresh hub
// seeded this way hands everything to every station that connects.
func ( hub *Store ) ImportFile( path string , now time.Time ) ( result ImportResult , err error ) {
	result.Header , result.Read , err = transfer.ReadFile( path , 500 , func( batch []syncproto.Envelope ) ( batch_err error ) {
		results , batch_err := hub.Push( batch , now )
		for _ , item := range results {
			switch item.Status {
			case syncproto.StatusOK:
				result.Stored++
			case syncproto.StatusStale:
				result.Stale++
			default:
				result.Rejected++
			}
		}
		return
	} )
	return
}
