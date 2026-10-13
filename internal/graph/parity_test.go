package graph

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The browser's timeline re-derives statuses from the dates every node carries.
// This golden graph is read by web/src/parity.test.ts, which replays "now" and
// must land on the statuses written here. Regenerate with UPDATE_GOLDEN=1.
func parityGraph(t *testing.T) *Graph {
	t.Helper()
	completed, notPlanned := "COMPLETED", "NOT_PLANNED"
	t0 := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) string { return t0.Add(d).Format(time.RFC3339) }
	closed := func(n *Issue, reason *string, after time.Duration) {
		n.StateReason = reason
		at := ts(after)
		n.ClosedAt = &at
	}

	epic := ghIssue(1, "Epic: Checkout rewrite", "OPEN", "Tracks the rewrite.", ts(0))
	done := ghIssue(2, "Cart totals", "CLOSED", "Part of #1.", ts(time.Hour))
	closed(&done, &completed, 48*time.Hour)
	review := ghIssue(3, "Payment form", "OPEN", "Part of #1.", ts(2*time.Hour))
	review.ClosedByPullRequestsReferences.Nodes = []PullRequest{{Number: 20, State: "OPEN", CreatedAt: ts(3 * time.Hour), UpdatedAt: ts(3 * time.Hour)}}
	mergedOpen := ghIssue(4, "Receipts", "OPEN", "Part of #1.", ts(4*time.Hour))
	mergedAt := ts(30 * time.Hour)
	mergedOpen.ClosedByPullRequestsReferences.Nodes = []PullRequest{{Number: 21, State: "MERGED", Merged: true, CreatedAt: ts(5 * time.Hour), UpdatedAt: mergedAt, MergedAt: &mergedAt, ClosedAt: &mergedAt}}
	mentioned := ghIssue(5, "Email templates", "OPEN", "Part of #1.", ts(6*time.Hour))
	if err := json.Unmarshal([]byte(`{"nodes":[{"source":{"__typename":"PullRequest","number":22,"state":"MERGED","merged":true,"createdAt":"2026-03-02T09:00:00Z","updatedAt":"2026-03-03T09:00:00Z","mergedAt":"2026-03-03T09:00:00Z","closedAt":"2026-03-03T09:00:00Z"}}]}`), &mentioned.TimelineItems); err != nil {
		t.Fatal(err)
	}
	blocked := ghIssue(6, "Tax rules", "OPEN", "Part of #1. Blocked by #3.", ts(8*time.Hour))
	dropped := ghIssue(7, "Gift wrap", "CLOSED", "Part of #1.", ts(9*time.Hour))
	closed(&dropped, &notPlanned, 72*time.Hour)
	loose := ghIssue(8, "Typo on the help page", "OPEN", "Standalone.", ts(10*time.Hour))

	return Build("example/shop", []Issue{epic, done, review, mergedOpen, mentioned, blocked, dropped, loose}, t0.Add(120*time.Hour))
}

func TestParityGolden(t *testing.T) {
	g := parityGraph(t)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(g); err != nil {
		t.Fatal(err)
	}
	const path = "testdata/parity.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Fatalf("%s is out of date; run UPDATE_GOLDEN=1 go test ./internal/graph", path)
	}
	// The statuses the fixture is meant to exercise.
	for num, want := range map[int]string{2: "done", 3: "in_review", 4: "merged_open", 5: "open", 6: "blocked", 7: "dropped", 8: "open"} {
		found := false
		for _, n := range g.Nodes {
			if n.Number == num {
				found = true
				if n.Status != want {
					t.Errorf("#%d status = %s, want %s", num, n.Status, want)
				}
			}
		}
		if !found {
			t.Errorf("#%d missing", num)
		}
	}
}
