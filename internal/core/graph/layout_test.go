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
