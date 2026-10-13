# Nurral-Issue-Mapper

Reads a GitHub repository's issues as a graph: which issue belongs to which epic,
what blocks what, which pull request is really finishing which issue, and what is
actually left. One Go binary; a terminal report and an interactive page.

## Why

A tracker with a few hundred issues records its structure in three different ways:
GitHub's native sub-issues, "Part of #12" in a body, and nothing at all on a series
titled "Checkout 3/8". Nobody holds that in their head. Issue Mapper reads all
three, builds one tree, and gives every issue one status:

| status | meaning |
|---|---|
| `done` / `dropped` | closed as completed / closed as not planned |
| `blocked` | depends on an issue that is still open |
| `in_review` | an open pull request is attached |
| `merged_open` | a merged pull request that **closes** it, but it is still open: probably needs closing |
| `in_progress`, `open` | everything else |

A pull request that only *mentions* an issue never marks it merged. Only a PR GitHub
links as closing it does.

## Quick start (Docker, one command)

You need Docker and a GitHub token that can read your repo's issues (see "Token"
below). Then:

```sh
docker run --rm -p 127.0.0.1:8080:8080 \
  -e GITHUB_TOKEN=<your token> \
  -e ISSUE_MAPPER_REPO=your-org/your-repo \
  ghcr.io/simulationcoder/nurral-issue-mapper
```

Open http://127.0.0.1:8080. That is the whole setup: no Go, no Node, no clone.

The same image runs the terminal reports; pass the command instead of the page:

```sh
docker run --rm -e GITHUB_TOKEN -e ISSUE_MAPPER_REPO=your-org/your-repo \
  ghcr.io/simulationcoder/nurral-issue-mapper merged-open
```

If you are logged in with the `gh` CLI, `-e GITHUB_TOKEN=$(gh auth token)` saves
creating a token (newer `gh` versions; older ones: `gh auth status --show-token`).

> The page has no login. Publish the port to `127.0.0.1` as above, not `0.0.0.0`,
> unless you put your own access control in front of it: it shows your private
> issues to anyone who can reach it.

### Token

- **Public repo:** any token works, including one with no scopes.
- **Private repo:** a classic token with the `repo` scope, or a fine-grained token on
  that repo with read access to *Issues*, *Pull requests* and *Metadata*.

Create one at https://github.com/settings/tokens.

## Install without Docker

```sh
git clone https://github.com/simulationcoder/nurral-issue-mapper
cd nurral-issue-mapper
make build            # needs Go 1.25+ and Node 20+ (Node only to build the page); writes bin/issue-mapper
```

## Use

It needs a GitHub token that can read the repo's issues: `GITHUB_TOKEN`, `GH_TOKEN`,
or a logged-in `gh` CLI. Run it inside a clone, or pass `--repo owner/name`.

```sh
issue-mapper                    # stats and the live epics
issue-mapper epic 12            # one epic's children by status, blockers, open PRs
issue-mapper issue 34           # parent chain, children, PRs, dependencies
issue-mapper open --blocked     # blocked issues and what blocks them
issue-mapper merged-open        # finished work nobody closed
issue-mapper orphans            # open issues nothing claims
issue-mapper json               # the raw graph
issue-mapper serve              # the interactive page on http://127.0.0.1:8080
```

`--refresh` re-reads GitHub; otherwise a result is reused for five minutes.

The page has a tree view of every epic with status colours, filters (hide finished,
pull requests, mentions), a detail panel, and a **Timeline** that replays the tracker
day by day.

## Configure

Optional `.issue-mapper.yml` (see `.issue-mapper.yml.example`):

```yaml
repo: owner/name
epic_title: '(?i)^theme:'          # titles that mark an epic
series_title: '^(.*?)\s*\b(\d{1,2})\s*/\s*(\d{1,2})\b\s*[:\-–—]'
```

## How the hierarchy is read

Strongest first, one primary parent per issue: GitHub's native sub-issue parent; a
body reference (`Part of #N`, `Epic #N`, `follow-up to`, `found via`); a numbered
title series opened within half an hour of an epic (inferred); the first issue the
body mentions (inferred). Dependencies (`blocked by`, `depends on`), `supersedes`,
related mentions and pull request `closes`/`references` edges are returned alongside.

## Known limits

- A PR that finishes an issue but merged into a non-default branch, or that wrote
  "Part of #N", is not one GitHub links as closing, so the issue stays `open`.
- The timeline uses today's "closes" links for past days.
- Only the first 3,000 issues are read (30 pages of 100); the response says when it
  was cut short.

## Develop

```sh
go test ./...                         # graph rules, config, report
cd web && npm test                    # the browser timeline against the Go golden graph
cd web && npm run dev                 # page on :5173, proxying /api to `issue-mapper serve`
UPDATE_GOLDEN=1 go test ./internal/graph   # after changing the graph output
```

The browser's Timeline derives statuses a second time from each node's dates. The Go
tests write `internal/graph/testdata/parity.json`; `web/src/parity.test.ts` replays
it and must agree.

## Licence

Apache-2.0. See `LICENSE`.
