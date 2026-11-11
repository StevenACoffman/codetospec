// Package communities checks domain assignment against the code's own
// structure: it groups files into communities of the import graph (gonum's
// Louvain modularity) and reports the domains whose files fall into several
// of them, a sign the domain lumps unrelated code together (every Go `main`
// package in one domain, a catch-all directory). Deterministic: sorted
// input, a fixed random source, gonum pinned in go.mod.
package communities

import (
	"cmp"
	"math/rand/v2"
	"path"
	"slices"
	"sort"
	"strings"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/iterator"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

// File is one source file and the domains of its chunks.
type File struct {
	Path      string
	Namespace string // as the chunker saw it; "" when the language has none
	Domains   []string
}

// Import is one file's import of a target, as an "import" fact states it.
type Import struct {
	From, Target string
}

// Split is a domain whose files fall into several communities.
type Split struct {
	Domain      string
	Communities int // communities holding at least minFiles of its files
	Files       int // files of the domain
}

// minFiles is how many of a domain's files a community must hold to count:
// a lone file in another community is noise, not a split.
const minFiles = 2

// Seeds of the random source Louvain uses to order its moves. Fixed, so a
// run is reproducible; any value would do.
const (
	seed1 uint64 = 0x636f6465746f7370 // "codetosp"
	seed2 uint64 = 0x636f6d6d756e6974 // "communist"
)

// Of is the community of each file of the import graph, by path.
//
// Requires: file paths are unique.
// Ensures:  deterministic for a given set of files and imports and a gonum
// version, whatever their order: edge weights are float sums, so imports
// are summed in a fixed order; communities are numbered from 0.
func Of(files []File, imports []Import) map[string]int {
	sorted := slices.Clone(files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	ordered := slices.Clone(imports)
	slices.SortFunc(ordered, func(a, b Import) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.Target, b.Target))
	})
	of := communityOf(importGraph(sorted, ordered))
	byPath := make(map[string]int, len(sorted))
	for i, f := range sorted {
		byPath[f.Path] = of[int64(i)]
	}
	return byPath
}

// Splits returns the domains whose files fall into several communities of
// of (see Of), most split first.
//
// Requires: of = Of(files, …).
// Ensures:  sorted by communities then domain.
func Splits(files []File, of map[string]int) []Split {
	held := make(map[string]map[int]int) // domain → community → files
	size := make(map[string]int)
	for _, f := range files {
		for _, d := range f.Domains {
			if held[d] == nil {
				held[d] = make(map[int]int)
			}
			held[d][of[f.Path]]++
			size[d]++
		}
	}
	var splits []Split
	for d, byCommunity := range held {
		n := 0
		for _, count := range byCommunity {
			if count >= minFiles {
				n++
			}
		}
		if n > 1 {
			splits = append(splits, Split{Domain: d, Communities: n, Files: size[d]})
		}
	}
	sort.Slice(splits, func(i, j int) bool {
		if splits[i].Communities != splits[j].Communities {
			return splits[i].Communities > splits[j].Communities
		}
		return splits[i].Domain < splits[j].Domain
	})
	return splits
}

// Edge weights: an import is spread over the files it reaches, so a large
// package does not outweigh a small one; files of one directory (one Go
// package, one PHP namespace folder) are tied together, since they rarely
// import each other.
const (
	importWeight    = 1.0
	directoryWeight = 2.0
)

// importGraph links the files (node i is files[i]) that import one another
// or share a directory, edges added in a fixed order.
func importGraph(files []File, imports []Import) *simple.WeightedUndirectedGraph {
	g := simple.NewWeightedUndirectedGraph(0, 0)
	for i := range files {
		g.AddNode(simple.Node(int64(i)))
	}
	weights := edgeWeights(newFileIndex(files), imports)
	pairs := make([][2]int64, 0, len(weights))
	for p := range weights {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i][0] < pairs[j][0] || pairs[i][0] == pairs[j][0] && pairs[i][1] < pairs[j][1]
	})
	for _, p := range pairs {
		g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(p[0]), simple.Node(p[1]), weights[p]))
	}
	return g
}

// edgeWeights sums the weight of every file pair (smaller id first) that an
// import or a shared directory links.
func edgeWeights(idx *fileIndex, imports []Import) map[[2]int64]float64 {
	weights := make(map[[2]int64]float64)
	link := func(a, b int64, w float64) {
		if a != b {
			weights[[2]int64{min(a, b), max(a, b)}] += w
		}
	}
	for _, imp := range imports {
		from, ok := idx.byPath[imp.From]
		if !ok {
			continue
		}
		targets := idx.resolve(imp.Target)
		for _, t := range targets {
			link(from, t, importWeight/float64(len(targets)))
		}
	}
	for _, d := range idx.dirs {
		members := idx.byDir[d]
		for i := range members {
			for j := i + 1; j < len(members); j++ {
				link(members[i], members[j], directoryWeight/float64(len(members)))
			}
		}
	}
	return weights
}

// fileIndex finds files (by node id) from a path, a directory or a
// namespace.
type fileIndex struct {
	byPath      map[string]int64
	byDir       map[string][]int64
	byNamespace map[string][]int64
	dirs        []string // sorted keys of byDir
}

func newFileIndex(files []File) *fileIndex {
	idx := &fileIndex{
		byPath:      make(map[string]int64, len(files)),
		byDir:       make(map[string][]int64),
		byNamespace: make(map[string][]int64),
	}
	for i, f := range files {
		id := int64(i)
		idx.byPath[f.Path] = id
		idx.byDir[path.Dir(f.Path)] = append(idx.byDir[path.Dir(f.Path)], id)
		if f.Namespace != "" {
			idx.byNamespace[f.Namespace] = append(idx.byNamespace[f.Namespace], id)
		}
	}
	for d := range idx.byDir {
		idx.dirs = append(idx.dirs, d)
	}
	sort.Strings(idx.dirs)
	return idx
}

// resolve returns the files an import target names, language-agnostic:
// files whose namespace is the target or holds it (App\Models\User names
// namespace App\Models), else files of the directory the target path ends
// with (Go: github.com/org/svc/rostering → rostering/).
func (idx *fileIndex) resolve(target string) []int64 {
	if ids := idx.byNamespace[target]; len(ids) > 0 {
		return ids
	}
	if i := strings.LastIndexAny(target, `\./:`); i > 0 {
		if ids := idx.byNamespace[strings.TrimRight(target[:i], `\./:`)]; len(ids) > 0 {
			return ids
		}
	}
	for _, d := range idx.dirs {
		if d != "." && strings.HasSuffix(target, "/"+d) {
			return idx.byDir[d]
		}
	}
	return nil
}

// communityOf assigns each node a community: Louvain's, split into
// connected parts so no community joins unconnected files (the guarantee
// Leiden adds over Louvain), numbered by their smallest node.
func communityOf(g *simple.WeightedUndirectedGraph) map[int64]int {
	reduced := community.Modularize(sortedGraph{g}, 1, rand.NewPCG(seed1, seed2))
	var parts [][]int64
	for _, members := range reduced.Communities() {
		for _, part := range topo.ConnectedComponents(induced(g, members)) {
			ids := make([]int64, len(part))
			for i, n := range part {
				ids[i] = n.ID()
			}
			slices.Sort(ids)
			parts = append(parts, ids)
		}
	}
	slices.SortFunc(parts, func(a, b []int64) int { return cmp.Compare(a[0], b[0]) })
	of := make(map[int64]int)
	for i, part := range parts {
		for _, id := range part {
			of[id] = i
		}
	}
	return of
}

// sortedGraph is a graph whose nodes and neighbours iterate by id. gonum's
// Louvain walks them in the order the graph gives, and a simple graph
// gives its maps' order: ties then broke differently on every call (on
// assignments, 9 or 10 communities from one input).
type sortedGraph struct {
	*simple.WeightedUndirectedGraph
}

func (g sortedGraph) Nodes() graph.Nodes {
	return byID(g.WeightedUndirectedGraph.Nodes())
}

func (g sortedGraph) From(id int64) graph.Nodes {
	return byID(g.WeightedUndirectedGraph.From(id))
}

func byID(it graph.Nodes) graph.Nodes {
	nodes := graph.NodesOf(it)
	slices.SortFunc(nodes, func(a, b graph.Node) int { return cmp.Compare(a.ID(), b.ID()) })
	return iterator.NewOrderedNodes(nodes)
}

// induced is the subgraph of g on members.
func induced(g graph.Undirected, members []graph.Node) graph.Undirected {
	sub := simple.NewUndirectedGraph()
	in := make(map[int64]bool, len(members))
	for _, n := range members {
		sub.AddNode(n)
		in[n.ID()] = true
	}
	for _, n := range members {
		neighbors := g.From(n.ID())
		for neighbors.Next() {
			if m := neighbors.Node(); in[m.ID()] && n.ID() < m.ID() {
				sub.SetEdge(simple.Edge{F: n, T: m})
			}
		}
	}
	return sub
}
