package d2

import "math"

// minIsolated is the fewest unconnected nodes that get a grid container:
// one alone already fits wherever ELK puts it.
const minIsolated = 2

// isolatedID is the node id of the grid container, apart from any id a
// caller derives from a name.
const isolatedID = "isolated:"

// GridIsolated is d with its top-level nodes no edge touches moved into
// one container labelled label, laid out as a square-ish grid: laid out
// by their edges, ELK would put them all in one row as wide as the
// diagram. d itself when there are fewer than two.
//
// Ensures: d is not modified; the container comes first.
func GridIsolated(d *Diagram, label string) *Diagram {
	touched := map[string]bool{}
	for _, e := range d.Edges {
		touched[e.From], touched[e.To] = true, true
	}
	unconnected := func(n *Node) bool {
		return n.In == "" && n.Class != ClassGroup && !touched[n.ID]
	}
	isolated := 0
	for i := range d.Nodes {
		if unconnected(&d.Nodes[i]) {
			isolated++
		}
	}
	if isolated < minIsolated {
		return d
	}
	columns := int(math.Ceil(math.Sqrt(float64(isolated))))
	out := &Diagram{Direction: d.Direction, Edges: d.Edges, Nodes: []Node{
		{ID: isolatedID, Label: label, Class: ClassGroup, Columns: columns},
	}}
	for _, n := range d.Nodes {
		if unconnected(&n) {
			n.In = isolatedID
		}
		out.Nodes = append(out.Nodes, n)
	}
	return out
}
