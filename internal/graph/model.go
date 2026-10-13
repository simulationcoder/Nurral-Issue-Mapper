package graph

// The issue graph: the GitHub issue tracker
// read as a graph, so "what is left under this epic" is a picture rather than
// a question.
//
// Hierarchy comes from three places, strongest first: GitHub's native
// sub-issue parent, a body reference ("Part of #878", "Epic #582"), and a
// title series ("Factor model 3/8") whose epic was opened in the same minute.
// Every relation found is returned as an edge; exactly one of them per issue is
// marked primary and that is the tree the UI lays out.

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type PR struct {
	Number int    `json:"number"`
	State  string `json:"state"` // OPEN, MERGED, CLOSED
	// Closes is true when GitHub links the PR as one that closes the issue. A PR
	// that only mentions the issue is listed too, but must not make it "done".
	Closes bool   `json:"closes"`
	URL    string `json:"url,omitempty"`
}

// Progress counts an issue's descendants along primary edges.
type Progress struct {
	Total   int `json:"total"`
	Done    int `json:"done"`
	Dropped int `json:"dropped"`
	Open    int `json:"open"`
}

type Node struct {
	// Kind is "issue" or "pr". A pull request is a node of its own, linked to
	// the issues it closes or references; it never counts as a child.
	Kind        string `json:"kind"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`        // open | closed
	StateReason string `json:"state_reason"` // completed | not_planned | duplicate | reopened | ""
	// Status is derived: done, dropped, blocked, in_review, merged_open,
	// in_progress, open for issues; pr_open, pr_merged, pr_closed for pull
	// requests. See BuildWith.
	Status         string   `json:"status"`
	URL            string   `json:"url"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	ClosedAt       *string  `json:"closed_at"`
	Labels         []Label  `json:"labels"`
	IsEpic         bool     `json:"is_epic"`
	Parent         *int     `json:"parent"`
	ParentKind     string   `json:"parent_kind,omitempty"`
	ParentInferred bool     `json:"parent_inferred,omitempty"`
	Children       []int    `json:"children"`
	Descendants    Progress `json:"descendants"`
	PullRequests   []PR     `json:"pull_requests"`
	// Issues lists, on a PR node, the issues it closes or references.
	Issues []int `json:"issues,omitempty"`
	// Series is the title prefix for "N/M"-numbered issues ("Factor model 8"),
	// so siblings can be recognised even when no epic claims them.
	Series  string `json:"series,omitempty"`
	Excerpt string `json:"excerpt"`
	Depth   int    `json:"depth"`
}

// Edge reads "From belongs to / depends on / follows To".
type Edge struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Kind     string `json:"kind"` // parent | part_of | series | follow_up | context | depends_on | supersedes | related | closes | references (PR -> issue)
	Inferred bool   `json:"inferred,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
}

type Stats struct {
	Open       int `json:"open"`
	Closed     int `json:"closed"`
	Done       int `json:"done"`
	Dropped    int `json:"dropped"`
	Blocked    int `json:"blocked"`
	InReview   int `json:"in_review"`
	MergedOpen int `json:"merged_open"`
	Epics      int `json:"epics"`
	Orphans    int `json:"orphans"`
	// Pull requests are counted apart from issues.
	PullRequests     int `json:"pull_requests"`
	OpenPullRequests int `json:"open_pull_requests"`
}

type Graph struct {
	Repo       string `json:"repo"`
	FetchedAt  string `json:"fetched_at"`
	Cached     bool   `json:"cached"`
	TTLSeconds int    `json:"ttl_seconds"`
	Truncated  bool   `json:"truncated,omitempty"`
	Nodes      []Node `json:"nodes"`
	Edges      []Edge `json:"edges"`
	// Roots are issues with children and no parent, most recently updated first.
	Roots []int `json:"roots"`
	// Orphans are open issues with neither parent nor children.
	Orphans []int `json:"orphans"`
	Stats   Stats `json:"stats"`
}
