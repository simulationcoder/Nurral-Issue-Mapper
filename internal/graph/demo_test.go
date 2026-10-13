package graph

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// demoGraph is an invented tracker ("acme/shop") for the README screenshot and
// for working on the page without a token. It holds no real issue.
func demoGraph(t *testing.T) *Graph {
	t.Helper()
	completed, notPlanned := "COMPLETED", "NOT_PLANNED"
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	day := func(d int) string { return t0.AddDate(0, 0, d).Format(time.RFC3339) }
	var issues []Issue
	add := func(n int, title, state, body string, created int) *Issue {
		is := ghIssue(n, title, state, body, day(created))
		issues = append(issues, is)
		return &issues[len(issues)-1]
	}
	closeAt := func(is *Issue, reason *string, d int) {
		is.StateReason = reason
		at := day(d)
		is.ClosedAt = &at
		is.UpdatedAt = at
	}
	pr := func(is *Issue, n int, state string, created, merged int) {
		p := PullRequest{Number: n, State: state, CreatedAt: day(created), UpdatedAt: day(created)}
		if state == "MERGED" {
			p.Merged = true
			m := day(merged)
			p.MergedAt, p.ClosedAt = &m, &m
			p.UpdatedAt = m
		}
		is.ClosedByPullRequestsReferences.Nodes = append(is.ClosedByPullRequestsReferences.Nodes, p)
	}

	// Epic 1: checkout, "Part of" in bodies.
	add(1, "Epic: Checkout rewrite", "OPEN", "Replace the legacy checkout flow.", 0)
	i := add(2, "Cart totals and rounding", "CLOSED", "Part of #1.", 2)
	closeAt(i, &completed, 12)
	pr(i, 101, "MERGED", 5, 12)
	i = add(3, "Address form with validation", "CLOSED", "Part of #1.", 3)
	closeAt(i, &completed, 20)
	pr(i, 102, "MERGED", 10, 20)
	i = add(4, "Payment form", "OPEN", "Part of #1. Blocked by #5.", 4)
	pr(i, 110, "OPEN", 40, 0)
	add(5, "Choose a payment provider", "OPEN", "Part of #1.", 5)
	add(6, "Tax rules by region", "OPEN", "Part of #1. Blocked by #5.", 6)
	i = add(7, "Gift wrap option", "CLOSED", "Part of #1.", 7)
	closeAt(i, &notPlanned, 30)
	i = add(8, "Order confirmation email", "OPEN", "Part of #1. Depends on #4.", 8)
	pr(i, 111, "MERGED", 50, 55)
	add(9, "Guest checkout", "OPEN", "Part of #1.", 9)

	// Epic 2: mobile app, native sub-issue parents.
	add(20, "Mobile app launch", "OPEN", "Ship the first mobile release.", 15)
	for k, c := range []struct {
		title, state string
		closed       int
	}{
		{"Sign-in screen", "CLOSED", 40},
		{"Push notifications", "OPEN", 0},
		{"Offline cart", "OPEN", 0},
		{"App store listing", "CLOSED", 60},
	} {
		is := add(21+k, c.title, c.state, "", 16+k)
		is.Parent = &struct {
			Number int `json:"number"`
		}{20}
		if c.state == "CLOSED" {
			closeAt(is, &completed, c.closed)
		}
	}

	// A numbered series opened with its epic.
	add(40, "Epic: Search", "OPEN", "Better product search.", 30)
	for k, c := range []struct {
		title, state string
		closed       int
	}{
		{"Search 1/4: index products", "CLOSED", 45},
		{"Search 2/4: ranking", "CLOSED", 62},
		{"Search 3/4: filters", "OPEN", 0},
		{"Search 4/4: typo tolerance", "OPEN", 0},
	} {
		is := add(41+k, c.title, c.state, "", 30)
		is.CreatedAt = t0.AddDate(0, 0, 30).Add(time.Duration(k+1) * time.Second).Format(time.RFC3339)
		if c.state == "CLOSED" {
			closeAt(is, &completed, c.closed)
		}
	}

	// Loose issues.
	add(60, "Typo on the help page", "OPEN", "", 50)
	add(61, "Dark mode for the dashboard", "OPEN", "Follow-up to #3.", 52)
	i = add(62, "Slow product page on mobile", "OPEN", "Found via #21.", 55)
	pr(i, 120, "OPEN", 70, 0)

	for k := range issues {
		issues[k].URL = "https://github.com/acme/shop/issues/" + itoaN(issues[k].Number)
	}
	return Build("acme/shop", issues, t0.AddDate(0, 0, 90))
}

func TestWriteDemoGraph(t *testing.T) {
	g := demoGraph(t)
	if len(g.Nodes) < 25 {
		t.Fatalf("demo graph has only %d nodes", len(g.Nodes))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(g); err != nil {
		t.Fatal(err)
	}
	const path = "testdata/demo.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, buf.Bytes()) {
		t.Fatalf("%s is out of date; run UPDATE_GOLDEN=1 go test ./internal/graph", path)
	}
	// The demo should show every status the page colours.
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		seen[n.Status] = true
	}
	for _, s := range []string{"done", "dropped", "blocked", "in_review", "open", "pr_open", "pr_merged"} {
		if !seen[s] {
			t.Errorf("demo graph has no %s", s)
		}
	}
}
