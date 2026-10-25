// Package hlc is a hybrid logical clock: the stamp every synced record
// carries, and the thing that decides which of two edits to the same record
// wins.
//
// A stamp is wall-clock milliseconds plus a counter. The clock never hands
// out a stamp at or below one it has already issued or observed, so on one
// machine stamps are strictly increasing even if the wall clock steps
// backwards, and a machine that has seen another's edit always stamps its
// next edit after it. Across machines whose clocks disagree, the later wall
// clock wins -- which is why the hub refuses stamps too far in the future and
// stations correct for a known offset from the hub.
package hlc

import (
	fmt "fmt"
	strconv "strconv"
	strings "strings"
	sync "sync"
	time "time"
)

// Stamp is one point on the clock. Its string form, "WWWWWWWWWWWWW-LLLLL"
// (zero-padded wall milliseconds, dash, zero-padded counter), sorts the same
// way the stamps compare, which keeps it readable in exports and logs.
type Stamp struct {
	Wall    int64
	Logical uint32
}

const maxLogical = 99999

func ( stamp Stamp ) String() ( result string ) {
	result = fmt.Sprintf( "%013d-%05d" , stamp.Wall , stamp.Logical )
	return
}

func ( stamp Stamp ) IsZero() ( result bool ) {
	result = stamp.Wall == 0 && stamp.Logical == 0
	return
}

// Time is the wall-clock part as a time.
func ( stamp Stamp ) Time() ( result time.Time ) {
	result = time.UnixMilli( stamp.Wall )
	return
}

// Compare returns -1, 0 or 1.
func ( stamp Stamp ) Compare( other Stamp ) ( result int ) {
	switch {
	case stamp.Wall < other.Wall:
		result = -1
	case stamp.Wall > other.Wall:
		result = 1
	case stamp.Logical < other.Logical:
		result = -1
	case stamp.Logical > other.Logical:
		result = 1
	}
	return
}

func ( stamp Stamp ) After( other Stamp ) ( result bool ) {
	result = stamp.Compare( other ) > 0
	return
}

// Parse reads the string form. The empty string is the zero stamp.
func Parse( text string ) ( stamp Stamp , err error ) {
	if text == "" { return }
	wall_text , logical_text , found := strings.Cut( text , "-" )
	if found == false {
		err = fmt.Errorf( "hlc: malformed stamp %q" , text )
		return
	}
	wall , err := strconv.ParseInt( wall_text , 10 , 64 )
	if err != nil {
		err = fmt.Errorf( "hlc: malformed stamp %q" , text )
		return
	}
	logical , err := strconv.ParseUint( logical_text , 10 , 32 )
	if err != nil || logical > maxLogical || wall < 0 {
		err = fmt.Errorf( "hlc: malformed stamp %q" , text )
		return
	}
	stamp = Stamp{ Wall: wall , Logical: uint32( logical ) }
	return
}

func ( stamp Stamp ) MarshalText() ( result []byte , err error ) {
	result = []byte( stamp.String() )
	return
}

func ( stamp *Stamp ) UnmarshalText( text []byte ) ( err error ) {
	*stamp , err = Parse( string( text ) )
	return
}

// FromTime is a stamp at a given wall time with counter zero -- used for
// records whose time of last edit is known from elsewhere (an import).
func FromTime( at time.Time ) ( stamp Stamp ) {
	stamp = Stamp{ Wall: at.UnixMilli() }
	return
}

// Clock issues stamps. It is safe for concurrent use.
type Clock struct {
	mu     sync.Mutex
	last   Stamp
	offset time.Duration
	now    func() time.Time
}

// maxObserveAhead bounds how far one observed stamp can drag this clock
// forward. Anything further ahead than this is a broken clock elsewhere; the
// record still applies (and wins), but this machine does not adopt the bogus
// time for every stamp it issues afterwards.
const maxObserveAhead = 24 * time.Hour

// New starts a clock that has already issued or seen last (the persisted
// high-water mark). now is time.Now outside tests.
func New( last Stamp , now func() time.Time ) ( clock *Clock ) {
	if now == nil {
		now = time.Now
	}
	clock = &Clock{ last: last , now: now }
	return
}

func ( clock *Clock ) physical() ( wall int64 ) {
	wall = clock.now().Add( clock.offset ).UnixMilli()
	return
}

// Now issues a stamp later than every stamp issued or observed so far.
func ( clock *Clock ) Now() ( stamp Stamp ) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	wall := clock.physical()
	if wall > clock.last.Wall {
		clock.last = Stamp{ Wall: wall }
	} else if clock.last.Logical >= maxLogical {
		// A burst of 100k stamps inside one millisecond: borrow the next
		// millisecond rather than overflow the counter's fixed width.
		clock.last = Stamp{ Wall: clock.last.Wall + 1 }
	} else {
		clock.last = Stamp{ Wall: clock.last.Wall , Logical: clock.last.Logical + 1 }
	}
	stamp = clock.last
	return
}

// Observe folds in a stamp seen on another machine's record, so the next
// local stamp sorts after it.
func ( clock *Clock ) Observe( remote Stamp ) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if remote.Wall > clock.physical()+maxObserveAhead.Milliseconds() {
		return
	}
	if remote.After( clock.last ) {
		clock.last = remote
	}
}

// Reset moves the high-water mark to to, even backwards. It is only for the
// one case where that is right: the hub refused this machine's stamps as
// being in the future, so the stamps above the corrected time were never
// accepted anywhere and nothing depends on them sorting first.
func ( clock *Clock ) Reset( to Stamp ) {
	clock.mu.Lock()
	clock.last = to
	clock.mu.Unlock()
}

// Last is the high-water mark, for persisting.
func ( clock *Clock ) Last() ( stamp Stamp ) {
	clock.mu.Lock()
	stamp = clock.last
	clock.mu.Unlock()
	return
}

// SetOffset corrects this machine's wall clock by d when stamping -- the
// station sets it to the measured difference from the hub's clock, so a
// laptop whose clock is wrong still stamps in the hub's timeline.
func ( clock *Clock ) SetOffset( d time.Duration ) {
	clock.mu.Lock()
	clock.offset = d
	clock.mu.Unlock()
}

func ( clock *Clock ) Offset() ( d time.Duration ) {
	clock.mu.Lock()
	d = clock.offset
	clock.mu.Unlock()
	return
}

// Corrected is the wall time with the offset applied: "now" in the hub's
// timeline. Visit times and day keys use it, so a laptop with a wrong clock
// still files today's visits under today.
func ( clock *Clock ) Corrected() ( at time.Time ) {
	clock.mu.Lock()
	at = clock.now().Add( clock.offset )
	clock.mu.Unlock()
	return
}

// Behind is how far the wall clock trails the high-water mark. More than a
// few seconds at startup means the machine's clock went backwards (or the
// database came from a machine whose clock was ahead).
func ( clock *Clock ) Behind() ( d time.Duration ) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	gap := clock.last.Wall - clock.physical()
	if gap > 0 {
		d = time.Duration( gap ) * time.Millisecond
	}
	return
}
