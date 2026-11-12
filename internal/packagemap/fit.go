package packagemap

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/d2"
)

// Fit says what Build did to a map with more edges than d2.MaxEdges, the
// most the renderer lays out: the zero value for a map drawn as it is.
type Fit struct {
	// Implied counts the edges left out because a longer path between the
	// same packages already says it: every dependency still reads from
	// the picture, along a path.
	Implied int
	// GroupDepth, when non-zero, is how many directory levels below the
	// prefix all packages share name a group: each node is a directory
	// standing for the packages under it, its label saying how many.
	GroupDepth int
}

// fitted is a map's nodes and edges after Fit.
type fitted struct {
	names map[string]int // node → how many packages it stands for
	edges [][2]string
	fit   Fit
}

// fitEdges makes edges between names drawable: as they are when there are
// at most d2.MaxEdges; else without the edges a longer path implies; else
// grouped by directory, at the deepest level whose reduced edges fit (the
// first level when none does).
//
// Requires: edges sorted (compareEdges), names the packages drawn.
// Ensures: deterministic; edges sorted; no edge from a node to itself.
func fitEdges(names []string, edges [][2]string, prefix string) fitted {
	one := make(map[string]int, len(names))
	for _, n := range names {
		one[n] = 1
	}
	if len(edges) <= d2.MaxEdges {
		return fitted{names: one, edges: edges}
	}
	reduced := reduce(edges)
	if len(reduced) <= d2.MaxEdges {
		return fitted{names: one, edges: reduced, fit: Fit{Implied: len(edges) - len(reduced)}}
	}
	var out fitted
	for depth := maxDepth(names, prefix) - 1; depth >= 1; depth-- {
		grouped, members := group(names, edges, prefix, depth)
		reduced = reduce(grouped)
		out = fitted{
			names: members, edges: reduced,
			fit: Fit{Implied: len(grouped) - len(reduced), GroupDepth: depth},
		}
		if len(reduced) <= d2.MaxEdges {
			break
		}
	}
	return out
}

// reduce removes, in order, each edge whose target its source still
// reaches without it: what is left says the same dependencies with the
// fewest edges (the transitive reduction of an acyclic graph, which Go
// imports are; grouping can make cycles, and the order keeps it
// deterministic there).
//
// Ensures: a node reaches the same nodes as before; edges keep their order.
func reduce(edges [][2]string) [][2]string {
	adj := map[string]map[string]bool{}
	for _, e := range edges {
		if adj[e[0]] == nil {
			adj[e[0]] = map[string]bool{}
		}
		adj[e[0]][e[1]] = true
	}
	var kept [][2]string
	for _, e := range edges {
		delete(adj[e[0]], e[1])
		if reaches(adj, e[0], e[1]) {
			continue
		}
		adj[e[0]][e[1]] = true
		kept = append(kept, e)
	}
	return kept
}

// reaches reports whether from reaches to along adj.
func reaches(adj map[string]map[string]bool, from, to string) bool {
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for m := range adj[n] {
			if m == to {
				return true
			}
			if !seen[m] {
				seen[m] = true
				queue = append(queue, m)
			}
		}
	}
	return false
}

// group maps each name to its first depth path segments below prefix (a
// directory holding one name stays that name), the edges between
// distinct groups (each pair once, sorted), and how many names each group
// stands for.
func group(
	names []string,
	edges [][2]string,
	prefix string,
	depth int,
) ([][2]string, map[string]int) {
	members := map[string][]string{}
	for _, n := range names {
		g := groupOf(n, prefix, depth)
		members[g] = append(members[g], n)
	}
	groupOfName := make(map[string]string, len(names))
	counts := make(map[string]int, len(members))
	for g, ns := range members {
		if len(ns) == 1 {
			g = ns[0]
		}
		counts[g] = len(ns)
		for _, n := range ns {
			groupOfName[n] = g
		}
	}
	pairs := map[[2]string]bool{}
	for _, e := range edges {
		from, to := groupOfName[e[0]], groupOfName[e[1]]
		if from != to {
			pairs[[2]string{from, to}] = true
		}
	}
	return slices.SortedFunc(maps.Keys(pairs), compareEdges), counts
}

// groupOf is name cut to its first depth segments below prefix; name
// itself when it has no more.
func groupOf(name, prefix string, depth int) string {
	rest := strings.TrimPrefix(name, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) <= depth {
		return name
	}
	return prefix + strings.Join(parts[:depth], "/")
}

// maxDepth is the most path segments any name has below prefix.
func maxDepth(names []string, prefix string) int {
	most := 0
	for _, n := range names {
		most = max(most, strings.Count(strings.TrimPrefix(n, prefix), "/")+1)
	}
	return most
}

// groupLabel is a node's label: the package's, or for a group of several,
// its directory, an ellipsis and how many packages it stands for.
func groupLabel(name, prefix string, members int) string {
	if members <= 1 {
		return label(name, prefix)
	}
	return label(name, prefix) + "/… (" + strconv.Itoa(members) + ")"
}
