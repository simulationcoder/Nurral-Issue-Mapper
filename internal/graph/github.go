package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// CacheTTL is how long a fetched graph is reused before GitHub is read again.
	CacheTTL     = 5 * time.Minute
	pageSize     = 100
	maxPages     = 30
	excerptRunes = 700
	graphQLURL   = "https://api.github.com/graphql"
)

// GitHub GraphQL shapes, only the fields the graph needs.
type Issue struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	State       string  `json:"state"`
	StateReason *string `json:"stateReason"`
	Body        string  `json:"body"`
	URL         string  `json:"url"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	ClosedAt    *string `json:"closedAt"`
	Parent      *struct {
		Number int `json:"number"`
	} `json:"parent"`
	SubIssuesSummary struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
	} `json:"subIssuesSummary"`
	Labels struct {
		Nodes []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"nodes"`
	} `json:"labels"`
	ClosedByPullRequestsReferences struct {
		Nodes []PullRequest `json:"nodes"`
	} `json:"closedByPullRequestsReferences"`
	TimelineItems struct {
		Nodes []struct {
			Source struct {
				Typename string `json:"__typename"`
				PullRequest
			} `json:"source"`
		} `json:"nodes"`
	} `json:"timelineItems"`
}

type PullRequest struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	State     string  `json:"state"`
	Merged    bool    `json:"merged"`
	URL       string  `json:"url"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
	MergedAt  *string `json:"mergedAt"`
	ClosedAt  *string `json:"closedAt"`
}

type issuesPage struct {
	Data struct {
		Repository *struct {
			Issues struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []Issue `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

const issuesQuery = `query($owner:String!, $name:String!, $cursor:String, $first:Int!) {
  repository(owner:$owner, name:$name) {
    issues(first:$first, after:$cursor, states:[OPEN,CLOSED], orderBy:{field:CREATED_AT, direction:ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state stateReason body url createdAt updatedAt closedAt
        parent { number }
        subIssuesSummary { total completed }
        labels(first:10) { nodes { name color } }
        closedByPullRequestsReferences(first:10) { nodes { number title state merged url createdAt updatedAt mergedAt closedAt } }
        timelineItems(first:60, itemTypes:[CROSS_REFERENCED_EVENT]) {
          nodes { ... on CrossReferencedEvent { source { __typename ... on PullRequest { number title state merged url createdAt updatedAt mergedAt closedAt } } } }
        }
      }
    }
  }
}`

// FetchOptions say which repository to read and how.
type FetchOptions struct {
	Repo       string       // owner/name
	Token      string       // GitHub token with read access to issues
	GraphQLURL string       // defaults to api.github.com; set for GitHub Enterprise or tests
	HTTPClient *http.Client // defaults to a 40 s client
}

// Fetch pages through every issue, open and closed. The bool is true when the
// page cap stopped it short.
func Fetch(ctx context.Context, o FetchOptions) ([]Issue, bool, error) {
	owner, name, ok := strings.Cut(o.Repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, false, fmt.Errorf("repo %q is not owner/name", o.Repo)
	}
	endpoint := strings.TrimSpace(o.GraphQLURL)
	if endpoint == "" {
		endpoint = graphQLURL
	}
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 40 * time.Second}
	}
	var all []Issue
	var cursor *string
	for range maxPages {
		variables := map[string]any{"owner": owner, "name": name, "first": pageSize}
		if cursor != nil {
			variables["cursor"] = *cursor
		}
		body, err := json.Marshal(map[string]any{"query": issuesQuery, "variables": variables})
		if err != nil {
			return nil, false, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, false, err
		}
		req.Header.Set("Authorization", "bearer "+strings.TrimSpace(o.Token))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "nurral-issue-mapper")
		res, err := client.Do(req)
		if err != nil {
			return nil, false, err
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
		res.Body.Close()
		if err != nil {
			return nil, false, err
		}
		if res.StatusCode != http.StatusOK {
			return nil, false, fmt.Errorf("graphql returned %d: %s", res.StatusCode, truncate(raw))
		}
		var page issuesPage
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, false, fmt.Errorf("decode graphql: %w", err)
		}
		if len(page.Errors) > 0 {
			return nil, false, fmt.Errorf("graphql: %s", page.Errors[0].Message)
		}
		if page.Data.Repository == nil {
			return nil, false, fmt.Errorf("repository %s not visible to the token", o.Repo)
		}
		issues := page.Data.Repository.Issues
		all = append(all, issues.Nodes...)
		if !issues.PageInfo.HasNextPage {
			return all, false, nil
		}
		next := issues.PageInfo.EndCursor
		cursor = &next
	}
	return all, true, nil
}

func truncate(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
