package graph

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func ghIssue(number int, title, state, body string, created string) Issue {
	n := Issue{Number: number, Title: title, State: state, Body: body, CreatedAt: created, UpdatedAt: created}
	n.URL = "https://github.com/x/y/issues/" + itoaN(number)
	return n
}

func nodeByNumber(t *testing.T, resp *Graph, n int) Node {
	t.Helper()
	for _, nd := range resp.Nodes {
		if nd.Number == n {
			return nd
		}
	}
	t.Fatalf("node #%d missing", n)
	return Node{}
}

func hasEdge(resp *Graph, from, to int, kind string) bool {
	for _, e := range resp.Edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func TestRelationsInBodyKeywords(t *testing.T) {
	known := map[int]bool{878: true, 880: true, 761: true, 832: true, 840: true, 457: true, 826: false, 851: true, 73: true}
	body := "Part of #878 (follows #880).\n\n**Blocked by** #761, #832 and #840.\n" +
		"PR #826 implements it. Needs validated exposure data (#457).\nFound via #851's trend feature. Related: #73"
	rels := relationsInBody(body, known)
	got := map[string]int{}
	for _, r := range rels {
		got[r.kind+":"+itoaN(r.target)]++
	}
	for _, want := range []string{"part_of:878", "follow_up:880", "depends_on:761", "depends_on:832", "depends_on:840", "follow_up:851", "related:73"} {
		if got[want] == 0 {
			t.Errorf("missing relation %s in %v", want, got)
		}
	}
	if got["depends_on:457"] != 0 {
		t.Errorf("'needs validated exposure data (#457' must not read as a dependency")
	}
	if got["part_of:826"] != 0 {
		t.Errorf("a PR number must never become an edge")
	}
}

func itoaN(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestMergedPRThatOnlyMentionsAnIssueDoesNotMarkItMergedOpen(t *testing.T) {
	t0 := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	mentioned := ghIssue(24, "Refine yield curve data", "OPEN", "Standalone.", t0.Format(time.RFC3339))
	if err := json.Unmarshal([]byte(`{"nodes":[{"source":{"__typename":"PullRequest","number":762,"state":"MERGED","merged":true}}]}`), &mentioned.TimelineItems); err != nil {
		t.Fatal(err)
	}
	closed := ghIssue(25, "Closed by its PR", "OPEN", "Standalone.", t0.Format(time.RFC3339))
	closed.ClosedByPullRequestsReferences.Nodes = []PullRequest{{Number: 763, State: "MERGED", Merged: true}}

	resp := Build("x/y", []Issue{mentioned, closed}, t0.Add(time.Hour))

	if got := nodeByNumber(t, resp, 24); got.Status != "open" {
		t.Errorf("a merged PR that only mentions #24 must leave it open, got %s", got.Status)
	}
	if got := nodeByNumber(t, resp, 24); len(got.PullRequests) != 1 || got.PullRequests[0].Closes {
		t.Errorf("the mention should still be listed, marked as not closing: %+v", got.PullRequests)
	}
	if got := nodeByNumber(t, resp, 25); got.Status != "merged_open" {
		t.Errorf("a merged PR that closes #25 should read merged_open, got %s", got.Status)
	}
}

func TestPRInBothSourcesKeepsClosesWhicheverComesFirst(t *testing.T) {
	t0 := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	both := ghIssue(30, "Both sources", "OPEN", "Standalone.", t0.Format(time.RFC3339))
	both.ClosedByPullRequestsReferences.Nodes = []PullRequest{{Number: 800, State: "MERGED", Merged: true}}
	if err := json.Unmarshal([]byte(`{"nodes":[{"source":{"__typename":"PullRequest","number":800,"state":"MERGED","merged":true}}]}`), &both.TimelineItems); err != nil {
		t.Fatal(err)
	}
	// An open PR plus a merged mention: in review, and the mention does not close.
	mixed := ghIssue(31, "Open PR and a mention", "OPEN", "Standalone.", t0.Format(time.RFC3339))
	mixed.ClosedByPullRequestsReferences.Nodes = []PullRequest{{Number: 801, State: "OPEN"}}
	if err := json.Unmarshal([]byte(`{"nodes":[{"source":{"__typename":"PullRequest","number":802,"state":"MERGED","merged":true}}]}`), &mixed.TimelineItems); err != nil {
		t.Fatal(err)
	}

	resp := Build("x/y", []Issue{both, mixed}, t0.Add(time.Hour))

	if got := nodeByNumber(t, resp, 30); len(got.PullRequests) != 1 || !got.PullRequests[0].Closes || got.Status != "merged_open" {
		t.Errorf("a PR seen as both closing and mentioning must close: %+v", got)
	}
	if got := nodeByNumber(t, resp, 31); got.Status != "in_review" {
		t.Errorf("open PR should give in_review, got %s", got.Status)
	}
}

func TestBuildIssueGraphHierarchyAndStatus(t *testing.T) {
	completed := "COMPLETED"
	notPlanned := "NOT_PLANNED"
	t0 := time.Date(2026, 10, 10, 8, 12, 36, 0, time.UTC)
	ts := func(d time.Duration) string { return t0.Add(d).Format(time.RFC3339) }

	epic := ghIssue(878, "Epic: Onboarding flow", "OPEN", "## Why\nA guided first-run section.", ts(-48*time.Hour))
	phase0 := ghIssue(879, "Phase 0", "CLOSED", "Part of #878.", ts(-47*time.Hour))
	phase0.StateReason = &completed
	phase0.Parent = &struct {
		Number int `json:"number"`
	}{878}
	phase1 := ghIssue(881, "Phase 2", "OPEN", "Part of #878. Depends on phase 0 and on #828's children.", ts(-46*time.Hour))
	phase1.ClosedByPullRequestsReferences.Nodes = []PullRequest{{Number: 949, State: "MERGED", Merged: true}}
	// A follow-up found during phase 0: no "Part of", only lineage.
	followUp := ghIssue(898, "Point-in-time screener", "OPEN", "Follow-up from #879.", ts(-20*time.Hour))
	followUp.ClosedByPullRequestsReferences.Nodes = []PullRequest{{Number: 902, State: "OPEN"}}
	// Blocked on an open issue.
	blocked := ghIssue(874, "Personalised benchmark", "OPEN", "Part of #878. **Blocked by** #881.", ts(-10*time.Hour))
	// Dropped.
	dup := ghIssue(300, "Duplicate thing", "CLOSED", "Duplicate of #879", ts(-30*time.Hour))
	dup.StateReason = &notPlanned
	// Series with its epic opened two seconds earlier.
	fEpic := ghIssue(922, "Epic: Factor research", "OPEN", "## Objective", ts(0))
	f1 := ghIssue(923, "Factor model 1/3: specify", "OPEN", "## Goal", ts(2*time.Second))
	f2 := ghIssue(924, "Factor model 2/3: build", "OPEN", "## Goal", ts(4*time.Second))
	f3 := ghIssue(930, "Factor research 3/3: expose", "CLOSED", "## Goal", ts(6*time.Second))
	f3.StateReason = &completed
	// Context-only: first mention in the opening paragraph.
	ctxOnly := ghIssue(917, "Fundamentals: total_debt", "OPEN", "## Problem\nIssue #898's coverage audit found total_debt missing.", ts(time.Hour))
	// Orphan.
	orphan := ghIssue(867, "Massive: erroneous trades", "OPEN", "Daily and minute aggregates disagree.", ts(2*time.Hour))
	// Cycle attempt: two issues each saying part of the other.
	a := ghIssue(401, "A", "OPEN", "Part of #402", ts(3*time.Hour))
	b := ghIssue(402, "B", "OPEN", "Part of #401", ts(3*time.Hour+time.Minute))

	resp := Build("x/y", []Issue{epic, phase0, phase1, followUp, blocked, dup, fEpic, f1, f2, f3, ctxOnly, orphan, a, b}, t0.Add(3*time.Hour))

	if got := nodeByNumber(t, resp, 879); got.Parent == nil || *got.Parent != 878 || got.ParentKind != "parent" || got.Status != "done" {
		t.Errorf("phase 0: %+v", got)
	}
	if got := nodeByNumber(t, resp, 881); got.Parent == nil || *got.Parent != 878 || got.ParentKind != "part_of" || got.Status != "merged_open" {
		t.Errorf("phase 2 should be part_of 878 with a merged PR but still open: %+v", got)
	}
	if hasEdge(resp, 881, 828, "context") || hasEdge(resp, 881, 828, "part_of") {
		t.Errorf("#828 is not in the tracker fixture and must not become an edge")
	}
	if got := nodeByNumber(t, resp, 898); got.Parent == nil || *got.Parent != 879 || got.ParentKind != "follow_up" || got.Status != "in_review" {
		t.Errorf("follow-up should hang under 879 and be in review: %+v", got)
	}
	if got := nodeByNumber(t, resp, 874); got.Status != "blocked" {
		t.Errorf("874 depends on open 881 and should be blocked, got %s", got.Status)
	}
	if !hasEdge(resp, 874, 881, "depends_on") {
		t.Errorf("missing depends_on edge 874 -> 881")
	}
	if got := nodeByNumber(t, resp, 300); got.Status != "dropped" {
		t.Errorf("not planned should be dropped, got %s", got.Status)
	}
	for _, n := range []int{923, 924, 930} {
		got := nodeByNumber(t, resp, n)
		if got.Parent == nil || *got.Parent != 922 || got.ParentKind != "series" || !got.ParentInferred {
			t.Errorf("series member %d should be inferred under 922: %+v", n, got)
		}
		if got.Series != "Factor model" {
			t.Errorf("series label for %d = %q", n, got.Series)
		}
	}
	if got := nodeByNumber(t, resp, 917); got.Parent == nil || *got.Parent != 898 || got.ParentKind != "context" || !got.ParentInferred {
		t.Errorf("context fallback should place 917 under 898: %+v", got)
	}
	if got := nodeByNumber(t, resp, 867); got.Parent != nil || len(got.Children) != 0 {
		t.Errorf("orphan should stand alone: %+v", got)
	}
	// The cycle resolves to exactly one primary edge.
	na, nb := nodeByNumber(t, resp, 401), nodeByNumber(t, resp, 402)
	if (na.Parent == nil) == (nb.Parent == nil) {
		t.Errorf("cycle must leave exactly one of 401/402 with a parent: %v %v", na.Parent, nb.Parent)
	}

	root := nodeByNumber(t, resp, 878)
	// 879, 881, 874 directly; 898 under 879; 917 under 898; and the duplicate 300
	// under 879 through the context fallback ("Duplicate of" is not hierarchy, but
	// its first mention is), counted as dropped.
	if root.Descendants.Total != 6 || root.Descendants.Done != 1 || root.Descendants.Dropped != 1 || root.Descendants.Open != 4 {
		t.Errorf("878 descendants = %+v", root.Descendants)
	}
	if got := nodeByNumber(t, resp, 300); got.Parent == nil || *got.Parent != 879 || got.ParentKind != "context" {
		t.Errorf("duplicate should hang under 879 via context: %+v", got)
	}
	if root.Status != "in_progress" || !root.IsEpic {
		t.Errorf("878 should be an in-progress epic: status=%s epic=%v", root.Status, root.IsEpic)
	}
	if got := nodeByNumber(t, resp, 917); got.Depth != 3 {
		t.Errorf("917 depth = %d, want 3 (878 > 879 > 898 > 917)", got.Depth)
	}
	roots := map[int]bool{}
	for _, r := range resp.Roots {
		roots[r] = true
	}
	if !roots[878] || !roots[922] || roots[879] || roots[867] {
		t.Errorf("roots = %v; want the two epics, not a child or an orphan", resp.Roots)
	}
	wantOrphans := map[int]bool{867: true}
	for _, o := range resp.Orphans {
		if !wantOrphans[o] && o != 401 && o != 402 {
			t.Errorf("unexpected orphan %d", o)
		}
	}
	if resp.Stats.Open != 11 || resp.Stats.Closed != 3 || resp.Stats.Blocked != 1 || resp.Stats.InReview != 1 || resp.Stats.MergedOpen != 1 {
		t.Errorf("stats = %+v", resp.Stats)
	}
	// Pull requests are nodes of their own, linked to the issue they close.
	if resp.Stats.PullRequests != 2 || resp.Stats.OpenPullRequests != 1 {
		t.Errorf("pr stats = %+v", resp.Stats)
	}
	pr := nodeByNumber(t, resp, 949)
	if pr.Kind != "pr" || pr.Status != "pr_merged" || pr.State != "closed" || len(pr.Issues) != 1 || pr.Issues[0] != 881 {
		t.Errorf("PR node 949: %+v", pr)
	}
	if !hasEdge(resp, 949, 881, "closes") || !hasEdge(resp, 902, 898, "closes") {
		t.Errorf("missing PR -> issue closes edges")
	}
	if nodeByNumber(t, resp, 902).Status != "pr_open" {
		t.Errorf("PR 902 should be open")
	}
	if nodeByNumber(t, resp, 881).Descendants.Total != 0 {
		t.Errorf("a PR must not count as a descendant")
	}
}

func TestIssueGraphHandlerCachesAndRefreshes(t *testing.T) {
	var calls int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("Authorization") != "bearer tok" {
			t.Errorf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		// Two pages, then stop.
		if _, paged := req.Variables["cursor"]; !paged {
			_, _ = w.Write([]byte(`{"data":{"repository":{"issues":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[{"number":1,"title":"Epic: one","state":"OPEN","body":"","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}]}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"issues":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"number":2,"title":"two","state":"CLOSED","stateReason":"COMPLETED","body":"Part of #1","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"}]}}}}`))
	}))
	defer gh.Close()

	svc := NewService(FetchOptions{Repo: "x/y", Token: "tok", GraphQLURL: gh.URL})
	first, err := svc.Get(context.Background(), false)
	if err != nil || len(first.Nodes) != 2 || first.Cached {
		t.Fatalf("first: err=%v nodes=%d", err, len(first.Nodes))
	}
	if first.Roots[0] != 1 || first.Nodes[1].Parent == nil || *first.Nodes[1].Parent != 1 {
		t.Fatalf("pagination lost the hierarchy: %+v", first)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 page fetches, got %d", calls)
	}
	second, err := svc.Get(context.Background(), false)
	if err != nil || !second.Cached || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("second call should be served from cache: err=%v calls=%d", err, calls)
	}
	third, err := svc.Get(context.Background(), true)
	if err != nil || third.Cached || atomic.LoadInt32(&calls) != 4 {
		t.Fatalf("refresh should refetch: err=%v calls=%d", err, calls)
	}
}

func TestServiceWithoutToken(t *testing.T) {
	if _, err := NewService(FetchOptions{Repo: "x/y"}).Get(context.Background(), false); !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

func TestServiceUpstreamError(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer gh.Close()
	_, err := NewService(FetchOptions{Repo: "x/y", Token: "bad", GraphQLURL: gh.URL}).Get(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want the 401 named", err)
	}
}
