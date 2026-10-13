// Package report prints a graph as plain text for the terminal.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/simulationcoder/nurral-issue-mapper/internal/graph"
)

var statusOrder = []string{"blocked", "in_review", "in_progress", "merged_open", "open", "done", "dropped"}

var statusLabel = map[string]string{
	"blocked":     "Blocked",
	"in_review":   "In review",
	"in_progress": "In progress",
	"merged_open": "PR merged, still open",
	"open":        "Open",
	"done":        "Done",
	"dropped":     "Not planned",
	"pr_open":     "PR open",
	"pr_merged":   "PR merged",
	"pr_closed":   "PR closed without merge",
}

var edgeLabel = map[string]string{
	"parent": "sub-issue", "part_of": "part of", "series": "series", "follow_up": "follow-up",
	"context": "mentions", "depends_on": "blocks", "supersedes": "supersedes", "related": "related",
	"closes": "closes", "references": "refs",
}

// View wraps a graph with the lookups the commands share.
type View struct {
	G     *graph.Graph
	nodes map[int]*graph.Node
}

// New indexes g.
func New(g *graph.Graph) *View {
	v := &View{G: g, nodes: map[int]*graph.Node{}}
	for i := range g.Nodes {
		v.nodes[g.Nodes[i].Number] = &g.Nodes[i]
	}
	return v
}

// Node returns the node or an error naming the repo.
func (v *View) Node(n int) (*graph.Node, error) {
	nd := v.nodes[n]
	if nd == nil {
		return nil, fmt.Errorf("#%d is not in the graph (not an issue or PR of %s)", n, v.G.Repo)
	}
	return nd, nil
}

// chain returns the ancestors of n, outermost first.
func (v *View) chain(n int) []*graph.Node {
	var out []*graph.Node
	seen := map[int]bool{}
	cur := v.nodes[n]
	for cur != nil && cur.Parent != nil && !seen[cur.Number] {
		seen[cur.Number] = true
		p := v.nodes[*cur.Parent]
		if p == nil {
			break
		}
		out = append([]*graph.Node{p}, out...)
		cur = p
	}
	return out
}

func (v *View) rootOf(n int) int {
	if c := v.chain(n); len(c) > 0 {
		return c[0].Number
	}
	return n
}

func (v *View) subtree(n int) []int {
	var out []int
	queue := []int{n}
	seen := map[int]bool{}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		nd := v.nodes[cur]
		if seen[cur] || nd == nil {
			continue
		}
		seen[cur] = true
		out = append(out, cur)
		queue = append(queue, nd.Children...)
	}
	return out
}

type related struct {
	other *graph.Node
	edge  graph.Edge
}

var relationHeadings = []string{"Blocked by", "Blocks", "Follow-ups", "Supersedes", "Superseded by", "Pull requests", "Related"}

func (v *View) relations(n int) map[string][]related {
	out := map[string][]related{}
	for _, e := range v.G.Edges {
		switch {
		case e.From == n:
			other := v.nodes[e.To]
			if other == nil {
				continue
			}
			switch e.Kind {
			case "depends_on":
				out["Blocked by"] = append(out["Blocked by"], related{other, e})
			case "supersedes":
				out["Supersedes"] = append(out["Supersedes"], related{other, e})
			case "related":
				out["Related"] = append(out["Related"], related{other, e})
			}
		case e.To == n:
			other := v.nodes[e.From]
			if other == nil {
				continue
			}
			switch e.Kind {
			case "depends_on":
				out["Blocks"] = append(out["Blocks"], related{other, e})
			case "follow_up":
				if !e.Primary {
					out["Follow-ups"] = append(out["Follow-ups"], related{other, e})
				}
			case "supersedes":
				out["Superseded by"] = append(out["Superseded by"], related{other, e})
			case "closes", "references":
				out["Pull requests"] = append(out["Pull requests"], related{other, e})
			case "related":
				out["Related"] = append(out["Related"], related{other, e})
			}
		}
	}
	return out
}

func ref(nd *graph.Node) string {
	if nd.Kind == "pr" {
		return fmt.Sprintf("PR #%d", nd.Number)
	}
	return fmt.Sprintf("#%d", nd.Number)
}

func label(status string) string {
	if l, ok := statusLabel[status]; ok {
		return l
	}
	return status
}

func clip(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}

func line(nd *graph.Node, width int, withStatus bool) string {
	s := fmt.Sprintf("%s  %s", ref(nd), clip(nd.Title, width))
	if withStatus {
		s += fmt.Sprintf("  [%s]", label(nd.Status))
	}
	return s
}

func progress(nd *graph.Node) string {
	d := nd.Descendants
	if d.Total == 0 {
		return ""
	}
	s := fmt.Sprintf("%d/%d done, %d open", d.Done, d.Total, d.Open)
	if d.Dropped > 0 {
		s += fmt.Sprintf(", %d dropped", d.Dropped)
	}
	return s
}

func day(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// Summary prints the stats and the live epics.
func (v *View) Summary(w io.Writer) {
	g, s := v.G, v.G.Stats
	cached := ""
	if g.Cached {
		cached = " (cached)"
	}
	fmt.Fprintf(w, "%s · %d nodes · %d edges · fetched %s%s\n", g.Repo, len(g.Nodes), len(g.Edges), g.FetchedAt, cached)
	fmt.Fprintf(w, "open %d · blocked %d · in review %d · PR merged still open %d · done %d · not planned %d · open PRs %d · unparented %d\n\n",
		s.Open, s.Blocked, s.InReview, s.MergedOpen, s.Done, s.Dropped, s.OpenPullRequests, s.Orphans)
	fmt.Fprintln(w, "Epics and parents (open first, most recently touched first):")
	closed := 0
	for _, n := range g.Roots {
		nd := v.nodes[n]
		if nd == nil {
			continue
		}
		if nd.State != "open" {
			closed++
			continue
		}
		fmt.Fprintf(w, "  %s  %s\n", line(nd, 64, true), progress(nd))
	}
	if closed > 0 {
		fmt.Fprintf(w, "  … and %d closed parents (`roots` shows them)\n", closed)
	}
	fmt.Fprintln(w, "\nNext: epic <n> · issue <n> · open --blocked · merged-open · orphans")
}

// Roots prints every epic and parent.
func (v *View) Roots(w io.Writer) {
	for _, n := range v.G.Roots {
		if nd := v.nodes[n]; nd != nil {
			fmt.Fprintf(w, "%s  %s\n", line(nd, 64, true), progress(nd))
		}
	}
}

// Epic prints one epic's descendants by status.
func (v *View) Epic(w io.Writer, num int) error {
	root, err := v.Node(num)
	if err != nil {
		return err
	}
	chain := v.chain(num)
	if len(chain) > 0 {
		parts := make([]string, len(chain))
		for i, p := range chain {
			parts[i] = fmt.Sprintf("#%d", p.Number)
		}
		fmt.Fprintln(w, "Belongs to: "+strings.Join(parts, " > "))
	}
	fmt.Fprintln(w, line(root, 90, true))
	if p := progress(root); p != "" {
		fmt.Fprintln(w, "Progress: "+p)
	}
	fmt.Fprintf(w, "GitHub: %s\n", root.URL)
	var members []*graph.Node
	for _, n := range v.subtree(num) {
		if n != num {
			members = append(members, v.nodes[n])
		}
	}
	if len(members) == 0 {
		fmt.Fprintln(w, "No children.")
	}
	byStatus := map[string][]*graph.Node{}
	for _, m := range members {
		byStatus[m.Status] = append(byStatus[m.Status], m)
	}
	for _, status := range statusOrder {
		items := byStatus[status]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s (%d)\n", label(status), len(items))
		sort.Slice(items, func(i, j int) bool { return items[i].Number < items[j].Number })
		for _, m := range items {
			depth := len(v.chain(m.Number)) - len(chain) - 1
			if depth < 0 {
				depth = 0
			}
			pad := strings.Repeat("  ", depth)
			extra := ""
			if m.Parent != nil && *m.Parent != num {
				extra = fmt.Sprintf("  (under #%d)", *m.Parent)
			}
			if m.ParentInferred {
				extra += "  (parent inferred)"
			}
			fmt.Fprintf(w, "  %s%s%s\n", pad, line(m, 70, false), extra)
			if status == "blocked" {
				for _, r := range v.relations(m.Number)["Blocked by"] {
					if r.other.State == "open" {
						fmt.Fprintf(w, "  %s    blocked by %s\n", pad, line(r.other, 60, true))
					}
				}
			}
			for _, p := range m.PullRequests {
				if p.State == "OPEN" {
					fmt.Fprintf(w, "  %s    open PR #%d\n", pad, p.Number)
				}
			}
		}
	}
	rel := v.relations(num)
	for _, heading := range []string{"Blocked by", "Blocks", "Pull requests"} {
		items := rel[heading]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", heading)
		for _, r := range items {
			note := ""
			if r.edge.Kind == "closes" || r.edge.Kind == "references" {
				note = fmt.Sprintf("  (%s)", edgeLabel[r.edge.Kind])
			}
			fmt.Fprintf(w, "  %s%s\n", line(r.other, 70, true), note)
		}
	}
	return nil
}

// Issue prints one issue or PR.
func (v *View) Issue(w io.Writer, num int) error {
	nd, err := v.Node(num)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, line(nd, 100, true))
	state := "State: " + nd.State
	if nd.StateReason != "" {
		state += " (" + nd.StateReason + ")"
	}
	state += " · opened " + day(nd.CreatedAt)
	if nd.ClosedAt != nil {
		state += " · closed " + day(*nd.ClosedAt)
	}
	fmt.Fprintln(w, state)
	if chain := v.chain(num); len(chain) > 0 {
		parts := make([]string, len(chain))
		for i, p := range chain {
			parts[i] = fmt.Sprintf("#%d %s", p.Number, clip(p.Title, 40))
		}
		via := edgeLabel[nd.ParentKind]
		if via == "" {
			via = nd.ParentKind
		}
		inf := ""
		if nd.ParentInferred {
			inf = ", inferred"
		}
		fmt.Fprintf(w, "Belongs to: %s  (via %s%s)\n", strings.Join(parts, " > "), via, inf)
	}
	if len(nd.Labels) > 0 {
		names := make([]string, len(nd.Labels))
		for i, l := range nd.Labels {
			names[i] = l.Name
		}
		fmt.Fprintln(w, "Labels: "+strings.Join(names, ", "))
	}
	if p := progress(nd); p != "" {
		fmt.Fprintln(w, "Progress: "+p)
	}
	if len(nd.Children) > 0 {
		fmt.Fprintf(w, "\nChildren (%d)\n", len(nd.Children))
		for _, c := range nd.Children {
			if child := v.nodes[c]; child != nil {
				fmt.Fprintf(w, "  %s\n", line(child, 70, true))
			}
		}
	}
	if nd.Kind == "pr" && len(nd.Issues) > 0 {
		fmt.Fprintln(w, "\nCloses or references")
		for _, i := range nd.Issues {
			if is := v.nodes[i]; is != nil {
				fmt.Fprintf(w, "  %s\n", line(is, 70, true))
			}
		}
	}
	rel := v.relations(num)
	for _, heading := range relationHeadings {
		items := rel[heading]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", heading)
		for _, r := range items {
			note := ""
			if r.edge.Inferred {
				note = "  (inferred)"
			}
			if r.edge.Kind == "closes" || r.edge.Kind == "references" {
				note = fmt.Sprintf("  (%s)", edgeLabel[r.edge.Kind])
			}
			fmt.Fprintf(w, "  %s%s\n", line(r.other, 70, true), note)
		}
	}
	if ex := strings.TrimSpace(nd.Excerpt); ex != "" {
		fmt.Fprintf(w, "\n%s\n", ex)
	}
	fmt.Fprintf(w, "\nGitHub: %s\n", nd.URL)
	return nil
}

func statusRank(s string) int {
	for i, o := range statusOrder {
		if o == s {
			return i
		}
	}
	return 9
}

// Open prints every open issue grouped by epic.
func (v *View) Open(w io.Writer, blockedOnly bool) {
	groups := map[int][]*graph.Node{}
	for i := range v.G.Nodes {
		nd := &v.G.Nodes[i]
		if nd.Kind != "issue" || nd.State != "open" || (blockedOnly && nd.Status != "blocked") {
			continue
		}
		r := v.rootOf(nd.Number)
		groups[r] = append(groups[r], nd)
	}
	if len(groups) == 0 {
		if blockedOnly {
			fmt.Fprintln(w, "Nothing blocked.")
		} else {
			fmt.Fprintln(w, "Nothing open.")
		}
		return
	}
	order := map[int]int{}
	for i, n := range v.G.Roots {
		order[n] = i
	}
	rank := func(r int) int {
		if o, ok := order[r]; ok {
			return o
		}
		return 10000
	}
	roots := make([]int, 0, len(groups))
	for r := range groups {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool {
		if rank(roots[i]) != rank(roots[j]) {
			return rank(roots[i]) < rank(roots[j])
		}
		return roots[i] < roots[j]
	})
	blockers := func(pad string, n int) {
		for _, r := range v.relations(n)["Blocked by"] {
			if r.other.State == "open" {
				fmt.Fprintf(w, "%sblocked by %s\n", pad, line(r.other, 60, true))
			}
		}
	}
	for _, root := range roots {
		items := groups[root]
		sort.Slice(items, func(i, j int) bool {
			if a, b := statusRank(items[i].Status), statusRank(items[j].Status); a != b {
				return a < b
			}
			return items[i].Number < items[j].Number
		})
		head := v.nodes[root]
		if len(items) == 1 && items[0].Number == root {
			fmt.Fprintln(w, line(head, 70, true))
			blockers("      ", root)
			fmt.Fprintln(w)
			continue
		}
		p := progress(head)
		if p != "" {
			p = "  " + p
		}
		fmt.Fprintf(w, "%s%s\n", line(head, 70, false), p)
		for _, nd := range items {
			if nd.Number == root {
				continue
			}
			fmt.Fprintf(w, "  %s\n", line(nd, 70, true))
			if nd.Status == "blocked" {
				blockers("      ", nd.Number)
			}
		}
		fmt.Fprintln(w)
	}
}

// MergedOpen prints open issues a merged, closing PR already names.
func (v *View) MergedOpen(w io.Writer) {
	var items []*graph.Node
	for i := range v.G.Nodes {
		if nd := &v.G.Nodes[i]; nd.Kind == "issue" && nd.Status == "merged_open" {
			items = append(items, nd)
		}
	}
	if len(items) == 0 {
		fmt.Fprintln(w, "None.")
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Number < items[j].Number })
	fmt.Fprintf(w, "%d open issues that a merged PR already closes. Usually finished work nobody closed.\n\n", len(items))
	for _, nd := range items {
		var merged []string
		for _, p := range nd.PullRequests {
			if p.State == "MERGED" && p.Closes {
				merged = append(merged, fmt.Sprintf("#%d", p.Number))
			}
		}
		under := ""
		if c := v.chain(nd.Number); len(c) > 0 {
			under = fmt.Sprintf("  (under #%d)", c[len(c)-1].Number)
		}
		fmt.Fprintf(w, "%s  merged PR %s%s\n", line(nd, 70, false), strings.Join(merged, ", "), under)
	}
}

// Orphans prints open issues nothing claims.
func (v *View) Orphans(w io.Writer) {
	var items []*graph.Node
	for _, n := range v.G.Orphans {
		if nd := v.nodes[n]; nd != nil {
			items = append(items, nd)
		}
	}
	fmt.Fprintf(w, "%d open issues with no parent and no children.\n\n", len(items))
	for _, nd := range items {
		fmt.Fprintln(w, line(nd, 80, true))
	}
}
