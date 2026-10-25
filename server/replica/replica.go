// Package replica keeps a station in step with the hub.
//
// Two loops run side by side. The pusher sends the outbox (everything
// written here that the hub has not yet confirmed) and is woken the moment
// anything is written, so another station sees a check-in within seconds. The
// puller asks the hub for everything after the last change it applied, every
// few seconds, a page at a time, and applies each page in the same
// transaction that advances its cursor.
//
// Three things can go wrong with the hub itself, and each has a plan:
//
//   - The hub was wiped or replaced. Its epoch changes; the station starts
//     its cursor over and re-sends every record it has, which is also how a
//     lost hub is rebuilt.
//   - This machine's clock is wrong. The hub refuses stamps too far in its
//     future; the station measures the difference from the hub's clock,
//     stamps in the hub's timeline from then on, and re-sends.
//   - The key was revoked. Sync stops and the dashboard says why.
package replica

import (
	bytes "bytes"
	context "context"
	json "encoding/json"
	errors "errors"
	fmt "fmt"
	io "io"
	log "log"
	http "net/http"
	strconv "strconv"
	sync "sync"
	time "time"

	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	hlc "github.com/0187773933/MastersClosetTracker/server/hlc"
	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
)

const (
	pushBatch     = 500
	pushBytes     = 400 * 1024
	maxResponse   = 64 << 20
	minBackoff    = 2 * time.Second
	maxBackoff    = 60 * time.Second
	offsetTrigger = time.Minute
	infoEvery     = 10 * time.Minute
)

var ErrUnauthorized = errors.New( "the hub refused this station's key (sync.api_key) -- it is unknown or was revoked" )

// Status is what the dashboard shows. The first four fields are the ones the
// existing page reads.
type Status struct {
	Enabled     bool      `json:"enabled"`
	Pending     int       `json:"pending"`
	LastSuccess time.Time `json:"last_success"`
	LastError   string    `json:"last_error"`

	Running      bool      `json:"running"`
	HubURL       string    `json:"hub_url"`
	Station      string    `json:"station"`
	LastAttempt  time.Time `json:"last_attempt"`
	Pulled       int       `json:"pulled"`
	Pushed       int       `json:"pushed"`
	TotalPulled  int       `json:"total_pulled"`
	TotalPushed  int       `json:"total_pushed"`
	Refused      int       `json:"refused"`
	Cursor       uint64    `json:"cursor"`
	MaxSeq       uint64    `json:"max_seq"`
	CatchingUp   bool      `json:"catching_up"`
	ClockOffset  int64     `json:"clock_offset_ms"`
	HubEpoch     string    `json:"hub_epoch"`
}

type Options struct {
	HubURL  string
	APIKey  string
	Station string
	Poll    time.Duration
	// Now is this machine's clock (time.Now outside tests).
	Now func() time.Time
	// Client is the HTTP client (a default with a 30s timeout if nil).
	Client *http.Client
}

type Replica struct {
	docs    *docs.Store
	options Options

	kick     chan struct{}
	push_mu  sync.Mutex
	pull_mu  sync.Mutex
	info_mu  sync.Mutex
	info_at  time.Time

	status_mu sync.Mutex
	status    Status

	cancel context.CancelFunc
	ctx    context.Context
	done   sync.WaitGroup
}

func New( docs_store *docs.Store , options Options ) ( replica *Replica ) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Client == nil {
		options.Client = &http.Client{ Timeout: 30 * time.Second }
	}
	if options.Poll <= 0 {
		options.Poll = 5 * time.Second
	}
	replica = &Replica{
		docs:    docs_store,
		options: options,
		kick:    make( chan struct{} , 1 ),
		ctx:     context.Background(),
	}
	replica.status = Status{ Enabled: true , HubURL: options.HubURL , Station: options.Station , HubEpoch: docs_store.GetMeta( docs.MetaHubEpoch ) }
	replica.status.Cursor = replica.cursor()
	docs_store.OnLocalWrite( replica.Kick )
	return
}

// Kick asks the pusher to run now. It never blocks.
func ( replica *Replica ) Kick() {
	select {
	case replica.kick <- struct{}{}:
	default:
	}
}

// Start runs the two loops until Stop.
func ( replica *Replica ) Start() {
	replica.ctx , replica.cancel = context.WithCancel( context.Background() )
	replica.done.Add( 2 )
	go replica.pushLoop()
	go replica.pullLoop()
	replica.Kick()
}

func ( replica *Replica ) Stop() {
	if replica.cancel != nil {
		replica.cancel()
	}
	replica.done.Wait()
}

func ( replica *Replica ) pushLoop() {
	defer replica.done.Done()
	backoff := time.Duration( 0 )
	retry := time.NewTimer( time.Hour )
	defer retry.Stop()
	for {
		select {
		case <-replica.ctx.Done():
			return
		case <-replica.kick:
		case <-retry.C:
		}
		if err := replica.Push(); err != nil {
			backoff = min( max( backoff*2 , minBackoff ) , maxBackoff )
			retry.Reset( backoff )
			continue
		}
		backoff = 0
	}
}

func ( replica *Replica ) pullLoop() {
	defer replica.done.Done()
	backoff := time.Duration( 0 )
	for {
		if err := replica.Pull(); err != nil {
			backoff = min( max( backoff*2 , minBackoff ) , maxBackoff )
		} else {
			backoff = 0
		}
		wait := max( replica.options.Poll , backoff )
		select {
		case <-replica.ctx.Done():
			return
		case <-time.After( wait ):
		}
	}
}

// SyncNow pushes everything and pulls until caught up, now. It is what the
// dashboard's button calls.
func ( replica *Replica ) SyncNow() ( result any ) {
	push_err := replica.Push()
	pull_err := replica.Pull()
	if push_err == nil && pull_err == nil {
		// A pull can reveal a new hub epoch, which re-queues everything.
		replica.Push()
	}
	result = replica.Status()
	return
}

// PullNow is a quick pull, for a record that may have been written at
// another station a moment ago.
func ( replica *Replica ) PullNow() {
	replica.Pull()
}

func ( replica *Replica ) Status() ( result any ) {
	replica.status_mu.Lock()
	status := replica.status
	replica.status_mu.Unlock()
	status.Pending = replica.docs.PendingCount()
	result = status
	return
}

// StatusValue is Status as its concrete type.
func ( replica *Replica ) StatusValue() ( status Status ) {
	status = replica.Status().( Status )
	return
}

func ( replica *Replica ) update( fn func( status *Status ) ) {
	replica.status_mu.Lock()
	fn( &replica.status )
	replica.status_mu.Unlock()
}

func ( replica *Replica ) record( err error ) {
	replica.update( func( status *Status ) {
		status.LastAttempt = replica.options.Now()
		if err != nil {
			status.LastError = err.Error()
			return
		}
		status.LastError = ""
		status.LastSuccess = replica.options.Now()
	} )
	if err != nil {
		log.Printf( "[sync] %v" , err )
	}
}

// ---------------------------------------------------------------------------
// talking to the hub
// ---------------------------------------------------------------------------

func ( replica *Replica ) call( method string , path string , body any , out any ) ( err error ) {
	var payload io.Reader
	if body != nil {
		encoded , encode_err := json.Marshal( body )
		if encode_err != nil {
			err = encode_err
			return
		}
		payload = bytes.NewReader( encoded )
	}
	request , err := http.NewRequestWithContext( replica.ctx , method , replica.options.HubURL+path , payload )
	if err != nil { return }
	request.Header.Set( "Authorization" , "Bearer "+replica.options.APIKey )
	if body != nil {
		request.Header.Set( "Content-Type" , "application/json" )
	}
	response , err := replica.options.Client.Do( request )
	if err != nil {
		err = fmt.Errorf( "hub unreachable: %w" , err )
		return
	}
	defer response.Body.Close()
	raw , err := io.ReadAll( io.LimitReader( response.Body , maxResponse ) )
	if err != nil { return }
	if response.StatusCode == http.StatusUnauthorized {
		err = ErrUnauthorized
		return
	}
	if response.StatusCode != http.StatusOK {
		var failure syncproto.ErrorResponse
		json.Unmarshal( raw , &failure )
		err = fmt.Errorf( "hub answered %d: %s" , response.StatusCode , failure.Error )
		return
	}
	err = json.Unmarshal( raw , out )
	return
}

// checkClock measures this machine's clock against the hub's and, if they
// differ by more than a minute, stamps in the hub's timeline from now on.
// It runs every few minutes and whenever the hub refuses a stamp.
func ( replica *Replica ) checkClock( force bool ) ( err error ) {
	replica.info_mu.Lock()
	defer replica.info_mu.Unlock()
	if force == false && replica.options.Now().Sub( replica.info_at ) < infoEvery {
		return
	}
	sent := replica.options.Now()
	var info syncproto.Info
	if err = replica.call( "GET" , "/sync/v1/info" , nil , &info ); err != nil { return }
	received := replica.options.Now()
	replica.info_at = received
	if info.Protocol != syncproto.Protocol {
		err = fmt.Errorf( "the hub speaks sync protocol %d, this station %d -- update the older one" , info.Protocol , syncproto.Protocol )
		return
	}
	// The hub read its clock about halfway through the round trip.
	midpoint := sent.Add( received.Sub( sent ) / 2 )
	offset := time.UnixMilli( info.ServerTime ).Sub( midpoint )
	clock := replica.docs.Clock()
	if offset > offsetTrigger || offset < -offsetTrigger {
		if clock.Offset() == 0 {
			log.Printf( "[sync] this machine's clock is %v off from the hub's; stamping in the hub's time -- fix the clock when you can" , offset.Round( time.Second ) )
		}
		clock.SetOffset( offset )
	} else {
		clock.SetOffset( 0 )
	}
	replica.update( func( status *Status ) { status.ClockOffset = clock.Offset().Milliseconds() } )
	return
}

// Push sends the outbox until it is empty.
func ( replica *Replica ) Push() ( err error ) {
	replica.push_mu.Lock()
	defer replica.push_mu.Unlock()
	defer func() { replica.record( err ) }()

	if err = replica.checkClock( false ); err != nil { return }
	pushed , refused := 0 , 0
	defer func() {
		replica.update( func( status *Status ) {
			status.Pushed = pushed
			status.TotalPushed += pushed
			status.Refused += refused
		} )
	}()
	for round := 0; round < 10_000; round++ {
		pending , pending_err := replica.docs.Pending( pushBatch , pushBytes )
		if pending_err != nil {
			err = pending_err
			return
		}
		if len( pending ) == 0 { return }
		var response syncproto.PushResponse
		if err = replica.call( "POST" , "/sync/v1/push" , syncproto.PushRequest{ Docs: pending } , &response ); err != nil { return }

		by_key := map[string]syncproto.Envelope{}
		for _ , envelope := range pending {
			by_key[ envelope.Key() ] = envelope
		}
		var done , future []syncproto.Envelope
		for _ , result := range response.Results {
			envelope , ok := by_key[ result.Collection+"\x00"+result.ID ]
			if ok == false { continue }
			switch result.Status {
			case syncproto.StatusOK , syncproto.StatusStale:
				done = append( done , envelope )
				pushed++
			case syncproto.StatusRejectedFuture:
				future = append( future , envelope )
			default:
				// Malformed or too large: the hub will never take it, and it
				// must not block everything queued behind it.
				log.Printf( "[sync] the hub refused %s/%s as invalid; dropping it from the outbox" , envelope.Collection , envelope.ID )
				done = append( done , envelope )
				refused++
			}
		}
		if err = replica.docs.MarkPushed( done ); err != nil { return }
		if len( future ) > 0 {
			if err = replica.restampFuture( future ); err != nil { return }
		}
		if len( done ) == 0 && len( future ) == 0 {
			err = errors.New( "the hub accepted none of a push; will retry" )
			return
		}
	}
	return
}

// restampFuture handles the hub refusing stamps from this machine's future:
// measure the clock, move this station's clock back into the hub's
// timeline, and give the refused records fresh stamps there.
func ( replica *Replica ) restampFuture( refused []syncproto.Envelope ) ( err error ) {
	if err = replica.checkClock( true ); err != nil { return }
	clock := replica.docs.Clock()
	clock.Reset( hlc.FromTime( clock.Corrected() ) )
	err = replica.docs.Restamp( refused )
	return
}

func ( replica *Replica ) cursor() ( cursor uint64 ) {
	cursor , _ = strconv.ParseUint( replica.docs.GetMeta( docs.MetaCursor ) , 10 , 64 )
	return
}

// Pull applies everything after the cursor, page by page.
func ( replica *Replica ) Pull() ( err error ) {
	replica.pull_mu.Lock()
	defer replica.pull_mu.Unlock()
	defer func() { replica.record( err ) }()

	// Stamps made here should be in the hub's timeline from the first one.
	if err = replica.checkClock( false ); err != nil { return }
	pulled := 0
	defer func() {
		replica.update( func( status *Status ) {
			status.Pulled = pulled
			status.TotalPulled += pulled
		} )
	}()
	for page_count := 0; page_count < 100_000; page_count++ {
		cursor := replica.cursor()
		var page syncproto.PullResponse
		if err = replica.call( "GET" , "/sync/v1/pull?since="+strconv.FormatUint( cursor , 10 ) , nil , &page ); err != nil { return }

		known := replica.docs.GetMeta( docs.MetaHubEpoch )
		if known != page.HubEpoch {
			if err = replica.adoptEpoch( known , page.HubEpoch ); err != nil { return }
			continue
		}
		incoming := make( []syncproto.Envelope , len( page.Docs ) )
		for index , doc := range page.Docs {
			incoming[ index ] = doc.Envelope
		}
		applied , _ , apply_err := replica.docs.ApplyPage( incoming , strconv.FormatUint( page.Next , 10 ) )
		if apply_err != nil {
			err = apply_err
			return
		}
		pulled += applied
		replica.update( func( status *Status ) {
			status.Cursor = page.Next
			status.MaxSeq = page.MaxSeq
			status.CatchingUp = page.HasMore
			status.HubEpoch = page.HubEpoch
		} )
		if page.HasMore == false { return }
	}
	return
}

// adoptEpoch follows the hub to a new database. The first time (no epoch
// known) is just a first sync. Any other change means the hub was replaced:
// start the cursor over and queue every record this station has, so the new
// hub ends up with all of it.
func ( replica *Replica ) adoptEpoch( known string , epoch string ) ( err error ) {
	if known != "" {
		log.Printf( "[sync] the hub's database changed (epoch %s -> %s); re-sending everything this station has" , known , epoch )
		queued , requeue_err := replica.docs.RequeueAll()
		if requeue_err != nil {
			err = requeue_err
			return
		}
		log.Printf( "[sync] queued %d records for the new hub" , queued )
	}
	if err = replica.docs.SetMeta( docs.MetaCursor , "0" ); err != nil { return }
	err = replica.docs.SetMeta( docs.MetaHubEpoch , epoch )
	replica.Kick()
	return
}
