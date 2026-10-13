package graph

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Graph derivation. Pure: everything below is a function of the fetched nodes.

// relationPattern finds "keyword ... #N" in a body. The gap between keyword and
// hash admits only non-word characters, so "needs #457" matches and "needs
// validated data (#457" does not -- the looser reading produced edges nobody
// meant.
type relationPattern struct {
	kind string
	re   *regexp.Regexp
}

const refGap = `[^\w#\n]{0,24}#(\d+)`

var relationPatterns = []relationPattern{
	{"part_of", regexp.MustCompile(`(?i)\b(?:part of|child of|sub-?issue of|sub-?task of|tracked (?:in|by)|belongs to|under|split (?:from|out of)|spun out of|phase \d+ of|step \d+ of|stage \d+ of|parent(?: issue)?)` + refGap)},
	{"part_of", regexp.MustCompile(`(?i)\bepic` + refGap)},
	{"follow_up", regexp.MustCompile(`(?i)\b(?:follow[- ]?ups? (?:to|of|from|on)|follows|found (?:via|by|in|during)|discovered (?:in|via|by|during)|surfaced (?:by|in)|left over from|leftover from|remaining from|continues)` + refGap)},
	{"depends_on", regexp.MustCompile(`(?i)\b(?:depends on|blocked (?:by|on)|blocks? on|waits? (?:on|for)|waiting (?:on|for)|needs|requires|after|once|prerequisite)` + refGap)},
	{"supersedes", regexp.MustCompile(`(?i)\b(?:supersedes|replaces|closes|fixes|resolves|obsoletes|duplicates?(?: of)?)` + refGap)},
	{"related", regexp.MustCompile(`(?i)\b(?:related(?: to| issues?)?|see also|see|cf|alongside|sibling of|compare|relates to|touches)` + refGap)},
}

// listTail continues a reference list: "#761, #832 and #840".
var listTail = regexp.MustCompile(`^\s*(?:,|;|/|&|and|or|\+)\s*(?:#|issue #?)(\d+)`)

// issueRef picks up bare "#N" mentions for the context fallback.
var issueRef = regexp.MustCompile(`#(\d+)`)

// Rules are the repo habits the graph reads titles by. The defaults suit most
// trackers; a .issue-mapper.yml overrides them.
type Rules struct {
	// SeriesTitle recognises "Factor model 3/8: ..." (capture 1 is the series
	// label, 2 the position, 3 the total).
	SeriesTitle *regexp.Regexp
	// EpicTitle: the issue calls itself an epic.
	EpicTitle *regexp.Regexp
}

// DefaultRules returns the built-in title conventions.
func DefaultRules() Rules {
	return Rules{
		SeriesTitle: regexp.MustCompile(`^(.*?)\s*\b(\d{1,2})\s*/\s*(\d{1,2})\b\s*[:\-–—]`),
		EpicTitle:   regexp.MustCompile(`(?i)(^|\W)epic(\W|$)`),
	}
}

type bodyRelation struct {
	kind   string
	target int
	pos    int
}

// relationsInBody returns every keyword-anchored reference, in body order.
// Only references to numbers in `known` count: pull requests share the number
// sequence, so "PR #826 implements ..." must not become an edge.
func relationsInBody(body string, known map[int]bool) []bodyRelation {
	var out []bodyRelation
	seen := map[[2]any]bool{}
	for _, p := range relationPatterns {
		for _, m := range p.re.FindAllStringSubmatchIndex(body, -1) {
			n, err := strconv.Atoi(body[m[2]:m[3]])
			if err != nil {
				continue
			}
			add := func(n int, pos int) {
				if !known[n] {
					return
				}
				key := [2]any{p.kind, n}
				if seen[key] {
					return
				}
				seen[key] = true
				out = append(out, bodyRelation{kind: p.kind, target: n, pos: pos})
			}
			add(n, m[0])
			// Chain: "#761, #832 and #840".
			rest := body[m[1]:]
			offset := m[1]
			for {
				t := listTail.FindStringSubmatchIndex(rest)
				if t == nil {
					break
				}
				tn, err := strconv.Atoi(rest[t[2]:t[3]])
				if err != nil {
					break
				}
				add(tn, offset+t[0])
				rest = rest[t[1]:]
				offset += t[1]
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].pos < out[j].pos })
	return out
}

// contextRef is the fallback: the first issue mentioned in the opening of the
// body, when the body says nothing structural. "Issue #852's coverage audit
// found ..." names where the work came from; it is flagged inferred.
func contextRef(body string, self int, known map[int]bool) (int, bool) {
	head := body
	if len(head) > 400 {
		head = head[:400]
	}
	for _, m := range issueRef.FindAllStringSubmatch(head, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n == self || !known[n] {
			continue
		}
		return n, true
	}
	return 0, false
}

// hierarchyRank orders candidate parents; lower wins.
func hierarchyRank(kind string) int {
	switch kind {
	case "parent":
		return 0
	case "part_of":
		return 1
	case "series":
		return 2
	case "follow_up":
		return 3
	case "context":
		return 4
	}
	return 99
}

func isHierarchyKind(kind string) bool { return hierarchyRank(kind) < 99 }

func excerpt(body string) string {
	s := strings.TrimSpace(body)
	// Light markdown clean-up so the panel reads as text.
	s = regexp.MustCompile(`(?m)^#{1,6}\s*`).ReplaceAllString(s, "")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	r := []rune(s)
	if len(r) > excerptRunes {
		return strings.TrimSpace(string(r[:excerptRunes])) + "…"
	}
	return s
}

func parseGitHubTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// buildIssueGraph derives nodes, edges, the primary tree and statuses.
// Build derives the graph with the default rules.
func Build(repo string, raw []Issue, now time.Time) *Graph {
	return BuildWith(DefaultRules(), repo, raw, now)
}

// BuildWith derives nodes, edges, the primary tree and statuses.
func BuildWith(rules Rules, repo string, raw []Issue, now time.Time) *Graph {
	seriesTitle, epicTitle := rules.SeriesTitle, rules.EpicTitle

	known := make(map[int]bool, len(raw))
	byNum := make(map[int]*Issue, len(raw))
	for i := range raw {
		known[raw[i].Number] = true
		byNum[raw[i].Number] = &raw[i]
	}

	type edgeKey struct {
		from, to int
		kind     string
	}
	edgeSet := map[edgeKey]*Edge{}
	var edges []*Edge
	addEdge := func(from, to int, kind string, inferred bool) {
		if from == to || !known[from] || !known[to] {
			return
		}
		k := edgeKey{from, to, kind}
		if e, ok := edgeSet[k]; ok {
			if !inferred {
				e.Inferred = false
			}
			return
		}
		e := &Edge{From: from, To: to, Kind: kind, Inferred: inferred}
		edgeSet[k] = e
		edges = append(edges, e)
	}

	// 1. Native parents and body relations.
	hasStructural := map[int]bool{}
	for _, n := range raw {
		if n.Parent != nil {
			addEdge(n.Number, n.Parent.Number, "parent", false)
			hasStructural[n.Number] = true
		}
		for _, rel := range relationsInBody(n.Body, known) {
			addEdge(n.Number, rel.target, rel.kind, false)
			if isHierarchyKind(rel.kind) {
				hasStructural[n.Number] = true
			}
		}
	}

	// 2. Title series ("Factor model 3/8"): siblings created together. An epic
	//    opened in the thirty minutes before the first member claims them.
	series := map[int]string{}
	type seriesMember struct {
		num, idx, total int
		created         time.Time
	}
	var members []seriesMember
	for _, n := range raw {
		m := seriesTitle.FindStringSubmatch(n.Title)
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[2])
		total, _ := strconv.Atoi(m[3])
		if total < 2 || idx < 1 || idx > total {
			continue
		}
		members = append(members, seriesMember{n.Number, idx, total, parseGitHubTime(n.CreatedAt)})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].created.Before(members[j].created) })
	var epics []Issue
	for _, n := range raw {
		if epicTitle.MatchString(n.Title) {
			epics = append(epics, n)
		}
	}
	used := map[int]bool{}
	for i := range members {
		if used[members[i].num] {
			continue
		}
		group := []seriesMember{members[i]}
		taken := map[int]bool{members[i].idx: true}
		used[members[i].num] = true
		for j := i + 1; j < len(members); j++ {
			mj := members[j]
			if used[mj.num] || mj.total != members[i].total || taken[mj.idx] {
				continue
			}
			if mj.created.Sub(members[i].created) > 6*time.Hour {
				break
			}
			group = append(group, mj)
			taken[mj.idx] = true
			used[mj.num] = true
		}
		if len(group) < 2 {
			continue
		}
		label := fmt.Sprintf("series of %d", members[i].total)
		if t := byNum[group[0].num]; t != nil {
			if m := seriesTitle.FindStringSubmatch(t.Title); m != nil && strings.TrimSpace(m[1]) != "" {
				label = strings.TrimSpace(m[1])
			}
		}
		for _, g := range group {
			series[g.num] = label
		}
		// Nearest epic opened shortly before the first member.
		first := group[0].created
		var best *Issue
		for k := range epics {
			e := &epics[k]
			ct := parseGitHubTime(e.CreatedAt)
			if ct.After(first) || first.Sub(ct) > 30*time.Minute {
				continue
			}
			if best == nil || ct.After(parseGitHubTime(best.CreatedAt)) {
				best = e
			}
		}
		if best == nil {
			continue
		}
		for _, g := range group {
			if g.num == best.Number {
				continue
			}
			addEdge(g.num, best.Number, "series", true)
			hasStructural[g.num] = true
		}
	}

	// 3. Context fallback for anything still unplaced.
	for _, n := range raw {
		if hasStructural[n.Number] {
			continue
		}
		if t, ok := contextRef(n.Body, n.Number, known); ok {
			addEdge(n.Number, t, "context", true)
		}
	}

	// 4. Choose one primary parent per issue, cycle-free.
	candidates := map[int][]*Edge{}
	for _, e := range edges {
		if isHierarchyKind(e.Kind) {
			candidates[e.From] = append(candidates[e.From], e)
		}
	}
	for n := range candidates {
		cs := candidates[n]
		sort.SliceStable(cs, func(i, j int) bool {
			ri, rj := hierarchyRank(cs[i].Kind), hierarchyRank(cs[j].Kind)
			if ri != rj {
				return ri < rj
			}
			if cs[i].Inferred != cs[j].Inferred {
				return !cs[i].Inferred
			}
			return cs[i].To > cs[j].To // newer issue as parent when otherwise tied
		})
	}
	parentOf := map[int]*Edge{}
	// Process strongest-first globally so native parents are placed before an
	// inferred edge could create the cycle that blocks them.
	order := make([]int, 0, len(candidates))
	for n := range candidates {
		order = append(order, n)
	}
	sort.Slice(order, func(i, j int) bool {
		ri, rj := hierarchyRank(candidates[order[i]][0].Kind), hierarchyRank(candidates[order[j]][0].Kind)
		if ri != rj {
			return ri < rj
		}
		return order[i] < order[j]
	})
	createsCycle := func(child, parent int) bool {
		seen := map[int]bool{child: true}
		for cur := parent; ; {
			if seen[cur] {
				return true
			}
			seen[cur] = true
			e, ok := parentOf[cur]
			if !ok {
				return false
			}
			cur = e.To
		}
	}
	for _, n := range order {
		for _, e := range candidates[n] {
			if createsCycle(n, e.To) {
				continue
			}
			e.Primary = true
			parentOf[n] = e
			break
		}
	}

	// 5. Nodes. Pull requests are nodes too, collected from every issue that
	//    links one; a PR closing two issues is one node with two edges.
	nodes := make([]Node, 0, len(raw))
	index := make(map[int]int, len(raw))
	children := map[int][]int{}
	for _, n := range raw {
		if e, ok := parentOf[n.Number]; ok {
			children[e.To] = append(children[e.To], n.Number)
		}
	}
	prNodes := map[int]*Node{}
	notePR := func(p PullRequest, issue int, closes bool) {
		if p.Number == 0 || known[p.Number] {
			return
		}
		nd, ok := prNodes[p.Number]
		if !ok {
			state := "open"
			status := "pr_open"
			if p.Merged || strings.EqualFold(p.State, "MERGED") {
				state, status = "closed", "pr_merged"
			} else if strings.EqualFold(p.State, "CLOSED") {
				state, status = "closed", "pr_closed"
			}
			closedAt := p.ClosedAt
			if p.MergedAt != nil {
				closedAt = p.MergedAt
			}
			nd = &Node{
				Kind:         "pr",
				Number:       p.Number,
				Title:        p.Title,
				State:        state,
				Status:       status,
				URL:          p.URL,
				CreatedAt:    p.CreatedAt,
				UpdatedAt:    p.UpdatedAt,
				ClosedAt:     closedAt,
				Labels:       []Label{},
				Children:     []int{},
				PullRequests: []PR{},
				Issues:       []int{},
			}
			prNodes[p.Number] = nd
		}
		kind := "references"
		if closes {
			kind = "closes"
		}
		// A PR can be reported as both a closer and a cross-reference; the
		// closing relation is the stronger and wins.
		for i, e := range edges {
			if e.From == p.Number && e.To == issue && (e.Kind == "closes" || e.Kind == "references") {
				if closes && e.Kind == "references" {
					delete(edgeSet, edgeKey{e.From, e.To, e.Kind})
					edges[i].Kind = "closes"
					edgeSet[edgeKey{e.From, e.To, "closes"}] = edges[i]
				}
				return
			}
		}
		k := edgeKey{p.Number, issue, kind}
		e := &Edge{From: p.Number, To: issue, Kind: kind}
		edgeSet[k] = e
		edges = append(edges, e)
		nd.Issues = append(nd.Issues, issue)
	}
	for _, n := range raw {
		node := Node{
			Kind:         "issue",
			Number:       n.Number,
			Title:        n.Title,
			State:        strings.ToLower(n.State),
			URL:          n.URL,
			CreatedAt:    n.CreatedAt,
			UpdatedAt:    n.UpdatedAt,
			ClosedAt:     n.ClosedAt,
			Labels:       []Label{},
			Children:     []int{},
			PullRequests: []PR{},
			Series:       series[n.Number],
			Excerpt:      excerpt(n.Body),
		}
		if n.StateReason != nil {
			node.StateReason = strings.ToLower(*n.StateReason)
		}
		for _, l := range n.Labels.Nodes {
			node.Labels = append(node.Labels, Label{Name: l.Name, Color: l.Color})
		}
		prSeen := map[int]int{} // PR number -> index in node.PullRequests
		addPR := func(p PullRequest, closes bool) {
			if p.Number == 0 {
				return
			}
			// The same PR can arrive from both sources; a closing link wins
			// whichever is read first.
			if i, ok := prSeen[p.Number]; ok {
				node.PullRequests[i].Closes = node.PullRequests[i].Closes || closes
				return
			}
			prSeen[p.Number] = len(node.PullRequests)
			state := strings.ToUpper(p.State)
			if p.Merged {
				state = "MERGED"
			}
			node.PullRequests = append(node.PullRequests, PR{Number: p.Number, State: state, URL: p.URL, Closes: closes})
		}
		for _, p := range n.ClosedByPullRequestsReferences.Nodes {
			addPR(p, true)
			notePR(p, n.Number, true)
		}
		for _, t := range n.TimelineItems.Nodes {
			if t.Source.Typename == "PullRequest" {
				addPR(t.Source.PullRequest, false)
				notePR(t.Source.PullRequest, n.Number, false)
			}
		}
		sort.Slice(node.PullRequests, func(i, j int) bool { return node.PullRequests[i].Number > node.PullRequests[j].Number })
		if e, ok := parentOf[n.Number]; ok {
			p := e.To
			node.Parent = &p
			node.ParentKind = e.Kind
			node.ParentInferred = e.Inferred
		}
		if cs := children[n.Number]; len(cs) > 0 {
			sort.Ints(cs)
			node.Children = cs
		}
		node.IsEpic = epicTitle.MatchString(n.Title) || n.SubIssuesSummary.Total > 0 || len(node.Children) >= 3
		index[n.Number] = len(nodes)
		nodes = append(nodes, node)
	}

	issueCount := len(nodes)

	// 6. Status. Blocked needs every node's base status first.
	base := func(nd *Node) string {
		if nd.State == "closed" {
			if nd.StateReason == "not_planned" || nd.StateReason == "duplicate" {
				return "dropped"
			}
			return "done"
		}
		merged, open := false, false
		for _, p := range nd.PullRequests {
			switch p.State {
			case "MERGED":
				// A merged PR that only mentions the issue says nothing about
				// whether it is finished.
				if p.Closes {
					merged = true
				}
			case "OPEN":
				open = true
			}
		}
		if open {
			return "in_review"
		}
		if merged && len(nd.Children) == 0 {
			return "merged_open"
		}
		return "open"
	}
	for i := range nodes {
		nodes[i].Status = base(&nodes[i])
	}
	for _, e := range edges {
		if e.Kind != "depends_on" {
			continue
		}
		fi, ok := index[e.From]
		if !ok {
			continue
		}
		ti, ok := index[e.To]
		if !ok {
			continue
		}
		if nodes[fi].Status == "open" && nodes[ti].State == "open" {
			nodes[fi].Status = "blocked"
		}
	}

	// 7. Descendant progress and depth, bottom-up along the primary tree.
	var progress func(num int, visiting map[int]bool) Progress
	progress = func(num int, visiting map[int]bool) Progress {
		var p Progress
		if visiting[num] {
			return p
		}
		visiting[num] = true
		for _, c := range children[num] {
			ci := index[c]
			cp := progress(c, visiting)
			p.Total += 1 + cp.Total
			p.Done += cp.Done
			p.Dropped += cp.Dropped
			p.Open += cp.Open
			switch nodes[ci].Status {
			case "done":
				p.Done++
			case "dropped":
				p.Dropped++
			default:
				p.Open++
			}
		}
		return p
	}
	for i := range nodes {
		nodes[i].Descendants = progress(nodes[i].Number, map[int]bool{})
		depth := 0
		for cur := nodes[i].Number; ; depth++ {
			e, ok := parentOf[cur]
			if !ok || depth > 50 {
				break
			}
			cur = e.To
		}
		nodes[i].Depth = depth
		if nodes[i].Status == "open" && len(nodes[i].Children) > 0 && nodes[i].Descendants.Done > 0 {
			nodes[i].Status = "in_progress"
		}
	}

	// PR nodes join after the issue statistics are settled; they take no part
	// in progress counts, roots or orphans.
	prNums := make([]int, 0, len(prNodes))
	for num := range prNodes {
		prNums = append(prNums, num)
	}
	sort.Ints(prNums)
	for _, num := range prNums {
		nd := prNodes[num]
		sort.Ints(nd.Issues)
		index[num] = len(nodes)
		nodes = append(nodes, *nd)
	}

	// 8. Roots, orphans, stats.
	resp := &Graph{
		Repo:       repo,
		FetchedAt:  now.Format(time.RFC3339),
		TTLSeconds: int(CacheTTL / time.Second),
		Nodes:      nodes,
		Edges:      make([]Edge, 0, len(edges)),
		Roots:      []int{},
		Orphans:    []int{},
	}
	for _, e := range edges {
		resp.Edges = append(resp.Edges, *e)
	}
	sort.Slice(resp.Edges, func(i, j int) bool {
		if resp.Edges[i].From != resp.Edges[j].From {
			return resp.Edges[i].From < resp.Edges[j].From
		}
		return resp.Edges[i].To < resp.Edges[j].To
	})
	resp.Stats.PullRequests = len(prNums)
	for i := range nodes {
		nd := &nodes[i]
		if i >= issueCount {
			if nd.Status == "pr_open" {
				resp.Stats.OpenPullRequests++
			}
			continue
		}
		switch nd.State {
		case "open":
			resp.Stats.Open++
		default:
			resp.Stats.Closed++
		}
		switch nd.Status {
		case "done":
			resp.Stats.Done++
		case "dropped":
			resp.Stats.Dropped++
		case "blocked":
			resp.Stats.Blocked++
		case "in_review":
			resp.Stats.InReview++
		case "merged_open":
			resp.Stats.MergedOpen++
		}
		if nd.IsEpic {
			resp.Stats.Epics++
		}
		if nd.Parent == nil && len(nd.Children) > 0 {
			resp.Roots = append(resp.Roots, nd.Number)
		}
		if nd.Parent == nil && len(nd.Children) == 0 && nd.State == "open" {
			resp.Orphans = append(resp.Orphans, nd.Number)
		}
	}
	resp.Stats.Orphans = len(resp.Orphans)
	// Roots: open and recently touched first, so the live epics lead.
	sort.SliceStable(resp.Roots, func(i, j int) bool {
		a, b := nodes[index[resp.Roots[i]]], nodes[index[resp.Roots[j]]]
		if (a.State == "open") != (b.State == "open") {
			return a.State == "open"
		}
		return a.UpdatedAt > b.UpdatedAt
	})
	sort.SliceStable(resp.Orphans, func(i, j int) bool {
		return nodes[index[resp.Orphans[i]]].UpdatedAt > nodes[index[resp.Orphans[j]]].UpdatedAt
	})
	return resp
}
