package graph

import "math"

// The drawing's geometry, in SVG user units. These place pixels, not
// decisions, so they are not doc 02 §13 rows (ADR-0050): the SVG scales to
// its container, and only the ratio between them shows.
const (
	// ViewSize is the side of the square viewBox.
	ViewSize = 480
	// RingRadius is the distance from the centre unit to each neighbour,
	// leaving room below the bottom node for its label.
	RingRadius = 170
)

// Point is a position in the viewBox, rounded to whole units so the SVG
// is identical on every platform.
type Point struct{ X, Y int }

// CentrePoint is where the centre unit is drawn.
func CentrePoint() Point { return Point{ViewSize / 2, ViewSize / 2} }

// Ring places n neighbours evenly on a circle around CentrePoint, the
// first at the top and the rest clockwise, in the order Neighbours ranked
// them. The layout is a function of n alone: no time, no randomness, no
// force pass, so the same neighbourhood is always drawn the same way.
func Ring(n int) []Point {
	c := CentrePoint()
	points := make([]Point, n)
	for i := range points {
		angle := 2*math.Pi*float64(i)/float64(n) - math.Pi/2
		points[i] = Point{
			X: c.X + int(math.Round(RingRadius*math.Cos(angle))),
			Y: c.Y + int(math.Round(RingRadius*math.Sin(angle))),
		}
	}
	return points
}

// ChordBend is how far, in viewBox units, the curve of a relation between
// two neighbours peaks beyond the straight segment joining them: enough to
// clear the centre node and its label when the two sit on opposite sides.
const ChordBend = 60

// ChordControl is the control point of the quadratic curve drawn for a
// relation between two neighbours a and b. A straight segment between two
// opposite neighbours would run through the centre node and read as two
// relations through it, so the curve bends away from the centre, its peak
// ChordBend beyond the segment's midpoint; when that midpoint is the
// centre itself, it bends perpendicular to the segment, to the left of a
// walker from a to b. Relations touching the centre stay straight.
func ChordControl(a, b Point) Point {
	c := CentrePoint()
	mx, my := float64(a.X+b.X)/2, float64(a.Y+b.Y)/2
	ux, uy := mx-float64(c.X), my-float64(c.Y)
	if l := math.Hypot(ux, uy); l >= 1 {
		ux, uy = ux/l, uy/l
	} else {
		dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
		l := math.Hypot(dx, dy)
		if l == 0 {
			return a
		}
		ux, uy = dy/l, -dx/l
	}
	// A quadratic curve peaks halfway between the midpoint and its control
	// point, so the control point sits twice the bend out.
	return Point{
		X: int(math.Round(mx + 2*ChordBend*ux)),
		Y: int(math.Round(my + 2*ChordBend*uy)),
	}
}
