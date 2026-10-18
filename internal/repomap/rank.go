package repomap

import (
	"cmp"
	"math"
	"path"
	"slices"
	"strings"
	"unicode"
)

type fileCount struct {
	file  int
	count int
}

// rankedTag is one entry of the ranked list: a definition, or a file alone
// (def < 0) for files whose definitions did not make it.
type rankedTag struct {
	file int
	def  int
}

// rankTags orders the definitions of files (paths, tags) by importance, as
// Aider's repo map does: files are nodes of a graph with an edge from each
// file naming an identifier to each file defining it, weighted by how
// telling the identifier is; PageRank, personalised to the focus files, ranks
// the files, and each file's rank flows to the definitions its edges point at.
// Unlike Aider's, edges join only files of one language family. Focus files
// are left out of the list.
func rankTags(paths []string, tags []fileTags, focus map[int]bool, symbols map[string]bool) []rankedTag {
	// Files are visited in order, so both lists stay sorted by file.
	defines := map[string][]int{}
	references := map[string][]fileCount{}
	for file, t := range tags {
		for _, def := range t.defs {
			if files := defines[def.name]; len(files) == 0 || files[len(files)-1] != file {
				defines[def.name] = append(files, file)
			}
		}
		for name, count := range t.refs {
			references[name] = append(references[name], fileCount{file, count})
		}
	}
	if len(references) == 0 {
		for name, files := range defines {
			for _, file := range files {
				references[name] = append(references[name], fileCount{file, 1})
			}
		}
	}

	families := make([]string, len(paths))
	for file, rel := range paths {
		families[file] = family(rel)
	}
	personal := personalization(paths, focus, symbols)
	g := newGraph(len(paths))
	idents := make([]string, 0, len(defines))
	for name := range defines {
		idents = append(idents, name)
	}
	slices.Sort(idents)
	// edges visits every edge once: from referencer to definer, weighted, for ident.
	edges := func(visit func(from, to int, weight float64, ident string)) {
		for _, ident := range idents {
			definers := defines[ident]
			refs, referenced := references[ident]
			if !referenced {
				for _, definer := range definers {
					visit(definer, definer, 0.1, ident)
				}
				continue
			}
			mul := identWeight(ident, symbols, len(definers))
			for _, ref := range refs {
				weight := mul * math.Sqrt(float64(ref.count))
				if focus[ref.file] {
					weight *= 50
				}
				for _, definer := range definers {
					if families[ref.file] == families[definer] {
						visit(ref.file, definer, weight, ident)
					}
				}
			}
		}
	}
	edges(g.add)
	rank := g.pageRank(personal)

	type key struct {
		file  int
		ident string
	}
	defRank := map[key]float64{}
	edges(func(from, to int, weight float64, ident string) {
		defRank[key{to, ident}] += rank[from] * weight / g.out[from]
	})
	keys := make([]key, 0, len(defRank))
	for k := range defRank {
		if !focus[k.file] {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(cmp.Compare(defRank[b], defRank[a]), strings.Compare(paths[a.file], paths[b.file]), strings.Compare(a.ident, b.ident))
	})

	var ranked []rankedTag
	listed := make([]bool, len(paths))
	for _, k := range keys {
		for i, def := range tags[k.file].defs {
			if def.name == k.ident {
				ranked = append(ranked, rankedTag{file: k.file, def: i})
			}
		}
		listed[k.file] = true
	}
	files := make([]int, len(paths))
	for i := range files {
		files[i] = i
	}
	slices.SortStableFunc(files, func(a, b int) int { return cmp.Compare(rank[b], rank[a]) })
	for _, file := range files {
		if !listed[file] && !focus[file] {
			ranked = append(ranked, rankedTag{file: file, def: -1})
		}
	}
	return ranked
}

// identWeight is Aider's multiplier for edges of an identifier: mentioned in
// the focus symbols ×10, a long snake, kebab or camel case name ×10, private
// ×0.1, defined in more than five files ×0.1.
func identWeight(ident string, symbols map[string]bool, definers int) float64 {
	mul := 1.0
	hasLetter := strings.IndexFunc(ident, unicode.IsLetter) >= 0
	snake := strings.Contains(ident, "_") && hasLetter
	kebab := strings.Contains(ident, "-") && hasLetter
	camel := strings.IndexFunc(ident, unicode.IsUpper) >= 0 && strings.IndexFunc(ident, unicode.IsLower) >= 0
	if symbols[ident] {
		mul *= 10
	}
	if (snake || kebab || camel) && len(ident) >= 8 {
		mul *= 10
	}
	if strings.HasPrefix(ident, "_") {
		mul *= 0.1
	}
	if definers > 5 {
		mul *= 0.1
	}
	return mul
}

// personalization gives each focus file, and each file whose path names a
// focus symbol, 100/len(paths); nil when there are none.
func personalization(paths []string, focus map[int]bool, symbols map[string]bool) []float64 {
	if len(focus) == 0 && len(symbols) == 0 {
		return nil
	}
	share := 100 / float64(len(paths))
	var personal []float64
	for file, rel := range paths {
		weight := 0.0
		if focus[file] {
			weight += share
		}
		base := path.Base(rel)
		named := symbols[base] || symbols[strings.TrimSuffix(base, path.Ext(base))]
		for element := range strings.SplitSeq(path.Dir(rel), "/") {
			named = named || symbols[element]
		}
		if named {
			weight += share
		}
		if weight > 0 {
			if personal == nil {
				personal = make([]float64, len(paths))
			}
			personal[file] = weight
		}
	}
	return personal
}

// graph is a weighted directed graph of files. Edges are summed per pair in
// the order they are added, so ranks do not depend on map order.
type graph struct {
	index []map[int]int
	adj   [][]edge
	out   []float64
}

type edge struct {
	to     int
	weight float64
}

func newGraph(nodes int) *graph {
	return &graph{index: make([]map[int]int, nodes), adj: make([][]edge, nodes), out: make([]float64, nodes)}
}

func (g *graph) add(from, to int, weight float64, _ string) {
	if g.index[from] == nil {
		g.index[from] = map[int]int{}
	}
	i, ok := g.index[from][to]
	if !ok {
		i = len(g.adj[from])
		g.index[from][to] = i
		g.adj[from] = append(g.adj[from], edge{to: to})
	}
	g.adj[from][i].weight += weight
	g.out[from] += weight
}

// pageRank is networkx's pagerank: damping 0.85, at most 100 iterations,
// tolerance 1e-6 per node; personal (nil for uniform) is both the teleport
// and the dangling-node distribution.
func (g *graph) pageRank(personal []float64) []float64 {
	n := len(g.adj)
	if n == 0 {
		return nil
	}
	p := make([]float64, n)
	total := 0.0
	for _, weight := range personal {
		total += weight
	}
	for i := range p {
		if total > 0 {
			p[i] = personal[i] / total
		} else {
			p[i] = 1 / float64(n)
		}
	}
	const alpha = 0.85
	x := make([]float64, n)
	for i := range x {
		x[i] = 1 / float64(n)
	}
	next := make([]float64, n)
	for range 100 {
		dangling := 0.0
		for i, out := range g.out {
			if out == 0 {
				dangling += x[i]
			}
		}
		for i := range next {
			next[i] = (alpha*dangling + 1 - alpha) * p[i]
		}
		for from, edges := range g.adj {
			for _, e := range edges {
				next[e.to] += alpha * x[from] * e.weight / g.out[from]
			}
		}
		diff := 0.0
		for i := range x {
			diff += math.Abs(next[i] - x[i])
		}
		x, next = next, x
		if diff < float64(n)*1e-6 {
			break
		}
	}
	return x
}
