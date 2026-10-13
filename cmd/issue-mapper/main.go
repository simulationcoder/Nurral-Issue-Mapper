// Command issue-mapper reads a GitHub repository's issues as a graph: which
// issue belongs to which epic, what blocks what, and what is really done.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/simulationcoder/nurral-issue-mapper/internal/config"
	"github.com/simulationcoder/nurral-issue-mapper/internal/graph"
	"github.com/simulationcoder/nurral-issue-mapper/internal/report"
	"github.com/simulationcoder/nurral-issue-mapper/internal/server"
	"github.com/simulationcoder/nurral-issue-mapper/web"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `issue-mapper reads a GitHub repo's issues as a graph.

Usage:
  issue-mapper [flags] [command]

Commands:
  summary          stats and the live epics (default)
  roots            every epic and parent, open first
  epic <n>         one epic's children by status, blockers and open PRs
  issue <n>        one issue or PR: parent chain, children, PRs, dependencies
  open [--blocked] every open issue grouped by epic
  merged-open      open issues a merged PR already closes
  orphans          open issues nothing claims
  json             the raw graph
  serve [--addr :8080]   the interactive page

Flags:
  --repo owner/name   default: $ISSUE_MAPPER_REPO, then .issue-mapper.yml, then the git origin remote
  --refresh           re-read GitHub instead of using the cache
  --config path       default: .issue-mapper.yml
  --demo              use an invented tracker: no token, no network (try it first)
  --version           print the version

Needs a GitHub token: GITHUB_TOKEN, GH_TOKEN, or a logged-in gh CLI.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "issue-mapper:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("issue-mapper", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	repo := fs.String("repo", "", "owner/name")
	refresh := fs.Bool("refresh", false, "re-read GitHub")
	cfgPath := fs.String("config", ".issue-mapper.yml", "config file")
	demo := fs.Bool("demo", false, "use an invented tracker")
	showVersion := fs.Bool("version", false, "print the version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("issue-mapper", version)
		return nil
	}
	rest := fs.Args()
	cmd := "summary"
	if len(rest) > 0 {
		cmd, rest = rest[0], rest[1:]
	}

	var svc *graph.Service
	var name string
	if *demo {
		svc, name = graph.NewDemoService(), "acme/shop (demo)"
	} else {
		file, err := config.Load(*cfgPath)
		if err != nil {
			return err
		}
		rules, err := file.Rules()
		if err != nil {
			return err
		}
		name = firstNonEmpty(*repo, os.Getenv("ISSUE_MAPPER_REPO"), file.Repo, config.RepoFromGit())
		if name == "" {
			return fmt.Errorf("no repo: pass --repo owner/name, set ISSUE_MAPPER_REPO, put it in %s, or run inside a clone", *cfgPath)
		}
		token := config.Token()
		if token == "" {
			return graph.ErrNoToken
		}
		svc = graph.NewService(graph.FetchOptions{Repo: name, Token: token})
		svc.Rules = rules
	}

	if cmd == "serve" {
		sf := flag.NewFlagSet("serve", flag.ContinueOnError)
		addr := sf.String("addr", "127.0.0.1:8080", "listen address")
		if err := sf.Parse(rest); err != nil {
			return err
		}
		warnIfExposed(*addr)
		fmt.Printf("issue-mapper: %s on http://%s\n", name, *addr)
		return (&http.Server{Addr: *addr, Handler: server.Handler(svc, web.Dist()), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	g, err := svc.Get(ctx, *refresh)
	if err != nil {
		return err
	}
	v := report.New(g)
	out := os.Stdout
	switch cmd {
	case "summary":
		v.Summary(out)
	case "roots":
		v.Roots(out)
	case "epic", "issue":
		if len(rest) != 1 {
			return fmt.Errorf("%s needs an issue number", cmd)
		}
		n, err := strconv.Atoi(rest[0])
		if err != nil {
			return fmt.Errorf("%q is not an issue number", rest[0])
		}
		if cmd == "epic" {
			return v.Epic(out, n)
		}
		return v.Issue(out, n)
	case "open":
		v.Open(out, len(rest) > 0 && rest[0] == "--blocked")
	case "merged-open":
		v.MergedOpen(out)
	case "orphans":
		v.Orphans(out)
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(g)
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// warnIfExposed says so when the page, which has no login, is bound beyond
// loopback. Inside Docker the container must bind all interfaces and the
// published port decides exposure, so it stays quiet there.
func warnIfExposed(addr string) {
	if inDocker() || isLoopback(addr) {
		return
	}
	fmt.Fprintf(os.Stderr, "issue-mapper: warning: %s is not a loopback address. The page has no login and shows every issue, private ones included, to anyone who can reach it.\n", addr)
}

func inDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// isLoopback reports whether a listen address only accepts local connections.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
