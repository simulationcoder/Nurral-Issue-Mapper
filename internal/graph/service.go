package graph

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNoToken means no GitHub token was configured.
var ErrNoToken = errors.New("no GitHub token: set GITHUB_TOKEN, or log in with `gh auth login`")

// Service fetches the tracker and keeps the derived graph for CacheTTL.
// Concurrent callers share one fetch.
type Service struct {
	Fetch FetchOptions
	Rules Rules

	demo *Graph // set by NewDemoService

	mu        sync.Mutex
	response  *Graph
	fetchedAt time.Time
}

// NewService returns a service with the default rules.
func NewService(o FetchOptions) *Service {
	return &Service{Fetch: o, Rules: DefaultRules()}
}

// Get returns the graph, from cache when it is fresh and force is false.
func (s *Service) Get(ctx context.Context, force bool) (*Graph, error) {
	if s.demo != nil {
		out := *s.demo
		out.FetchedAt = time.Now().UTC().Format(time.RFC3339)
		return &out, nil
	}
	if s.Fetch.Token == "" {
		return nil, ErrNoToken
	}
	// Holding the lock across the fetch is what coalesces concurrent misses.
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force && s.response != nil && time.Since(s.fetchedAt) < CacheTTL {
		out := *s.response
		out.Cached = true
		return &out, nil
	}
	issues, truncated, err := Fetch(ctx, s.Fetch)
	if err != nil {
		return nil, err
	}
	built := BuildWith(s.Rules, s.Fetch.Repo, issues, time.Now().UTC())
	built.Truncated = truncated
	s.response, s.fetchedAt = built, time.Now()
	out := *built
	return &out, nil
}
