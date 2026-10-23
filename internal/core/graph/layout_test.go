package graph

import (
	"reflect"
	"testing"
)

func TestRing_PlacesNeighboursClockwiseFromTheTop(t *testing.T) {
	c := CentrePoint()
	cases := map[int][]Point{
		0: {},
		1: {{c.X, c.Y - RingRadius}},
		2: {{c.X, c.Y - RingRadius}, {c.X, c.Y + RingRadius}},
		4: {
			{c.X, c.Y - RingRadius},
			{c.X + RingRadius, c.Y},
			{c.X, c.Y + RingRadius},
			{c.X - RingRadius, c.Y},
		},
	}
	for n, want := range cases {
		if got := Ring(n); !reflect.DeepEqual(got, want) {
			t.Fatalf("Ring(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestRing_EveryPointLiesOnTheRingInsideTheView(t *testing.T) {
	c := CentrePoint()
	for n := 1; n <= MaxNeighbours; n++ {
		points := Ring(n)
		if len(points) != n {
			t.Fatalf("Ring(%d) has %d points", n, len(points))
		}
		seen := map[Point]bool{}
		for _, p := range points {
			dx, dy := p.X-c.X, p.Y-c.Y
			// Integer rounding moves a point by at most one unit per axis.
			if d2 := dx*dx + dy*dy; d2 < (RingRadius-1)*(RingRadius-1) || d2 > (RingRadius+1)*(RingRadius+1) {
				t.Fatalf("Ring(%d) point %v is not on the ring (d^2 = %d)", n, p, d2)
			}
			if p.X < 0 || p.Y < 0 || p.X > ViewSize || p.Y > ViewSize {
				t.Fatalf("Ring(%d) point %v is outside the %d view", n, p, ViewSize)
			}
			if seen[p] {
				t.Fatalf("Ring(%d) places two neighbours at %v", n, p)
			}
			seen[p] = true
		}
	}
}

func TestCentrePoint_IsTheMiddleOfTheView(t *testing.T) {
	if got := CentrePoint(); got != (Point{ViewSize / 2, ViewSize / 2}) {
		t.Fatalf("CentrePoint() = %v", got)
	}
}

// distance2 is the squared distance between two points.
func distance2(a, b Point) int { dx, dy := a.X-b.X, a.Y-b.Y; return dx*dx + dy*dy }

// curveApex is where a quadratic curve from a to b through control c
// peaks: its point at t = 1/2.
func curveApex(a, c, b Point) Point { return Point{(a.X + 2*c.X + b.X) / 4, (a.Y + 2*c.Y + b.Y) / 4} }

func TestChordControl_ACurveBetweenOppositeNeighboursMissesTheCentre(t *testing.T) {
	c := CentrePoint()
	top, bottom := Point{c.X, c.Y - RingRadius}, Point{c.X, c.Y + RingRadius}

	ctrl := ChordControl(top, bottom)
	apex := curveApex(top, ctrl, bottom)

	if d2 := distance2(apex, c); d2 < (ChordBend-1)*(ChordBend-1) {
		t.Fatalf("apex %v is %d^2 from the centre, want at least ChordBend (%d)", apex, d2, ChordBend)
	}
	if apex.Y != c.Y {
		t.Fatalf("apex %v is not level with the centre: the bend must be perpendicular to the chord", apex)
	}
}

func TestChordControl_ACurveBetweenNeighboursBendsAwayFromTheCentre(t *testing.T) {
	c := CentrePoint()
	for n := 3; n <= MaxNeighbours; n++ {
		ring := Ring(n)
		a, b := ring[0], ring[1]
		mid := Point{(a.X + b.X) / 2, (a.Y + b.Y) / 2}
		apex := curveApex(a, ChordControl(a, b), b)
		if distance2(apex, c) <= distance2(mid, c) {
			t.Fatalf("Ring(%d): apex %v is no further from the centre than the straight chord's midpoint %v", n, apex, mid)
		}
	}
}

func TestChordControl_ASegmentOfNoLengthHasNoBend(t *testing.T) {
	c := CentrePoint()
	if got := ChordControl(c, c); got != c {
		t.Fatalf("ChordControl(c, c) = %v, want %v", got, c)
	}
}
