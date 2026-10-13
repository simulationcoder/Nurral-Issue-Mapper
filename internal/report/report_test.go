package report

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/simulationcoder/nurral-issue-mapper/internal/graph"
)

func golden(t *testing.T) *View {
	t.Helper()
	raw, err := os.ReadFile("../graph/testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var g graph.Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return New(&g)
}

func TestEpicGroupsChildrenByStatus(t *testing.T) {
	var b bytes.Buffer
	if err := golden(t).Epic(&b, 1); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"Blocked (1)", "In review (1)", "PR merged, still open (1)", "#6  Tax rules", "blocked by #3", "open PR #20"} {
		if !strings.Contains(out, want) {
			t.Errorf("epic output lacks %q:\n%s", want, out)
		}
	}
}

func TestMergedOpenListsOnlyClosingPRs(t *testing.T) {
	var b bytes.Buffer
	golden(t).MergedOpen(&b)
	out := b.String()
	if !strings.Contains(out, "#4  Receipts") || !strings.Contains(out, "merged PR #21") {
		t.Errorf("missing the issue a closing PR merged for:\n%s", out)
	}
	if strings.Contains(out, "Email templates") {
		t.Errorf("a PR that only mentions an issue must not list it:\n%s", out)
	}
}

func TestUnknownIssueNamesTheRepo(t *testing.T) {
	var b bytes.Buffer
	err := golden(t).Issue(&b, 999)
	if err == nil || !strings.Contains(err.Error(), "example/shop") {
		t.Fatalf("err = %v", err)
	}
}
