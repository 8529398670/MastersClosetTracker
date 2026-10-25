package hlc

import (
	testing "testing"
	time "time"
)

func TestStampStringSortsLikeCompare( t *testing.T ) {
	stamps := []Stamp{ { 5 , 0 } , { 5 , 1 } , { 6 , 0 } , { 1700000000000 , 99999 } , { 1700000000001 , 0 } }
	for index := 1; index < len( stamps ); index++ {
		a , b := stamps[ index-1 ] , stamps[ index ]
		if a.Compare( b ) >= 0 || a.String() >= b.String() {
			t.Fatalf( "%v should sort before %v (strings %q %q)" , a , b , a.String() , b.String() )
		}
		parsed , err := Parse( b.String() )
		if err != nil || parsed != b {
			t.Fatalf( "round trip %v -> %q -> %v (%v)" , b , b.String() , parsed , err )
		}
	}
}

func TestNowIsStrictlyIncreasingWhenWallClockGoesBack( t *testing.T ) {
	wall := time.UnixMilli( 1_000_000 )
	clock := New( Stamp{} , func() time.Time { return wall } )
	first := clock.Now()
	wall = wall.Add( -time.Hour )
	second := clock.Now()
	third := clock.Now()
	if second.After( first ) == false || third.After( second ) == false {
		t.Fatalf( "stamps not increasing: %v %v %v" , first , second , third )
	}
	if clock.Behind() < 59*time.Minute {
		t.Fatalf( "Behind = %v, want about an hour" , clock.Behind() )
	}
}

func TestObserveMovesPastRemoteButIgnoresAbsurdFuture( t *testing.T ) {
	wall := time.UnixMilli( 1_000_000 )
	clock := New( Stamp{} , func() time.Time { return wall } )
	remote := Stamp{ Wall: 1_000_500 , Logical: 3 }
	clock.Observe( remote )
	if next := clock.Now(); next.After( remote ) == false {
		t.Fatalf( "next stamp %v does not follow observed %v" , next , remote )
	}
	absurd := Stamp{ Wall: wall.Add( 48 * time.Hour ).UnixMilli() }
	clock.Observe( absurd )
	if clock.Last().After( absurd ) || clock.Last() == absurd {
		t.Fatalf( "clock adopted a stamp two days ahead" )
	}
}

func TestCounterOverflowBorrowsNextMillisecond( t *testing.T ) {
	wall := time.UnixMilli( 2_000 )
	clock := New( Stamp{ Wall: 2_000 , Logical: maxLogical } , func() time.Time { return wall } )
	next := clock.Now()
	if next.Wall != 2_001 || next.Logical != 0 {
		t.Fatalf( "got %v, want 2001-0" , next )
	}
}

func TestOffsetAndReset( t *testing.T ) {
	wall := time.UnixMilli( 10_000_000 )
	clock := New( Stamp{} , func() time.Time { return wall } )
	clock.SetOffset( 10 * time.Minute )
	ahead := clock.Now()
	if ahead.Wall != wall.Add( 10*time.Minute ).UnixMilli() {
		t.Fatalf( "offset not applied: %v" , ahead )
	}
	clock.SetOffset( 0 )
	clock.Reset( FromTime( clock.Corrected() ) )
	if next := clock.Now(); next.Wall > wall.UnixMilli()+1 {
		t.Fatalf( "after Reset the clock should be back at wall time, got %v" , next )
	}
}
