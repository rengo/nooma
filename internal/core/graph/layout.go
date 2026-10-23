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
