package ui

import (
	"fmt"
	"sort"
	"strings"
)

const (
	emptyTree = "(no modules)\n"

	branch     = "├── "
	lastBranch = "└── "
	trunk      = "│   "
	blank      = "    "

	pathSep    = "/"
	trailerSep = ", "
	countMark  = "+"
)

// Row is one line of a tree or flat list.
type Row struct {
	Category  []string
	Name      string // sorted and addressed by this
	Mark      string // drawn right after Name, inside its underline or strike
	Count     string // drawn after Mark as "+ <Count>" in the quiet connector color
	Marker    string
	Color     Color
	Rank      int // sorts before Name
	Underline bool
	Strike    bool
	Note      string
	NoteColor Color
	Trailer   []string // shown dim as "  ← a, b"
	Children  []Row
}

// SortRows orders rows by rank, then name (byte order), stable.
func SortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Rank != rows[j].Rank {
			return rows[i].Rank < rows[j].Rank
		}
		return rows[i].Name < rows[j].Name
	})
}

// name is a row's name as drawn: Mark appended, struck through or underlined,
// then the Count.
func (r Row) name(pal Palette) string {
	name := r.Name + r.Mark
	switch {
	case r.Strike:
		name = pal.strike + name + pal.reset
	case r.Underline:
		name = pal.underline + name + pal.reset
	}
	if r.Count != "" {
		name += fmt.Sprintf("%s%s %s%s", countMark, pal.line, r.Count, pal.reset)
	}
	return name
}

func (r Row) path() string {
	if len(r.Category) == 0 {
		return r.Name
	}
	return strings.Join(r.Category, pathSep) + pathSep + r.Name
}

func (r Row) trailer(pal Palette) string {
	if len(r.Trailer) == 0 {
		return ""
	}
	return fmt.Sprintf("  %s← %s%s", pal.line, strings.Join(r.Trailer, trailerSep), pal.reset)
}

type treeNode struct {
	rows     []Row
	children map[string]*treeNode
}

func buildTree(rows []Row) *treeNode {
	root := &treeNode{children: map[string]*treeNode{}}
	for _, r := range rows {
		node := root
		for _, seg := range r.Category {
			child, ok := node.children[seg]
			if !ok {
				child = &treeNode{children: map[string]*treeNode{}}
				node.children[seg] = child
			}
			node = child
		}
		node.rows = append(node.rows, r)
	}
	return root
}

// Tree writes rows to w as a tree nested under rootLabel: rows before
// subcategories at each level, each group ordered by SortRows.
func Tree(w *strings.Builder, rows []Row, rootLabel string, pal Palette) {
	if len(rows) == 0 {
		w.WriteString(emptyTree)
		return
	}

	fmt.Fprintf(w, "%s%s%s\n", pal.title, rootLabel, pal.reset)
	renderNode(w, buildTree(rows), "", pal)
	w.WriteString("\n")
}

// renderNode prints node's rows, then its subcategories (alphabetical), each
// continuing with prefix.
func renderNode(w *strings.Builder, node *treeNode, prefix string, pal Palette) {
	SortRows(node.rows)
	cats := make([]string, 0, len(node.children))
	for c := range node.children {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	total := len(node.rows) + len(cats)
	i := 0

	for _, r := range node.rows {
		i++
		connector, childPrefix := branch, prefix+trunk
		if i == total {
			connector, childPrefix = lastBranch, prefix+blank
		}
		renderRow(w, r, prefix, connector, childPrefix, pal)
	}

	for _, cat := range cats {
		i++
		connector, childPrefix := branch, prefix+trunk
		if i == total {
			connector, childPrefix = lastBranch, prefix+blank
		}
		fmt.Fprintf(w, "%s%s%s%s%s%s/%s\n", pal.line, prefix, connector, pal.reset, pal.title, cat, pal.reset)
		renderNode(w, node.children[cat], childPrefix, pal)
	}
}

func renderRow(w *strings.Builder, r Row, prefix, connector, childPrefix string, pal Palette) {
	fmt.Fprintf(w, "%s%s%s%s%s%s %s%s", pal.line, prefix, connector, pal.reset, pal.Tint(r.Color), r.Marker, r.name(pal), pal.reset)
	if r.Note != "" {
		fmt.Fprintf(w, " %s%s%s", pal.Tint(r.NoteColor), r.Note, pal.reset)
	}
	w.WriteString(r.trailer(pal))
	w.WriteString("\n")

	SortRows(r.Children)
	for i, c := range r.Children {
		childConnector, grandPrefix := branch, childPrefix+trunk
		if i == len(r.Children)-1 {
			childConnector, grandPrefix = lastBranch, childPrefix+blank
		}
		renderRow(w, c, childPrefix, childConnector, grandPrefix, pal)
	}
}

// Flat writes rows to w as a flat, colored list: one "marker name" per line,
// sorted by full path so entries group by category.
func Flat(w *strings.Builder, rows []Row, pal Palette) {
	if len(rows) == 0 {
		w.WriteString(emptyTree)
		return
	}

	sorted := append([]Row(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path() < sorted[j].path() })
	for _, r := range sorted {
		fmt.Fprintf(w, "%s%s %s%s", pal.Tint(r.Color), r.Marker, r.name(pal), pal.reset)
		w.WriteString(r.trailer(pal))
		w.WriteString("\n")
	}
}
