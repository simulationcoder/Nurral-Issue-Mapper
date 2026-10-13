import type { IssueGraphEdge, IssueGraphNode, IssueGraphResponse, IssueGraphStatus } from "./types";

// The tracker as it stood on a past date, rebuilt in the browser from the
// dates every node already carries: an issue exists once opened, is closed once
// its closed_at has passed, a pull request is open until merged or closed.
// Status, progress, roots, orphans and the stats are then re-derived with the
// same rules the API uses today (internal/api/handlers/issue_graph.go, steps
// 6-8), so a node reads the same whether the map shows today or last March.
//
// GitHub records when an issue opened and closed, not when it became blocked
// or went into review, so those two are inferred from the dates of the PRs and
// dependencies around it.

export type TimeRange = { start: number; end: number };

const DAY = 86_400_000;

export const dayFloor = (ms: number) => Math.floor(ms / DAY) * DAY;

const ts = (iso: string | null | undefined) => (iso ? Date.parse(iso) : NaN);

export function timelineRange(data: IssueGraphResponse): TimeRange {
  let start = Infinity;
  for (const n of data.nodes) {
    const c = ts(n.created_at);
    if (Number.isFinite(c) && c < start) start = c;
  }
  const fetched = ts(data.fetched_at);
  const end = Number.isFinite(fetched) ? fetched : Date.now();
  if (!Number.isFinite(start)) start = end - DAY;
  return { start: dayFloor(start), end };
}

export function graphAsOf(data: IssueGraphResponse, at: number): IssueGraphResponse {
  const live = new Map<number, IssueGraphNode>();
  for (const n of data.nodes) live.set(n.number, n);

  const exists = (n: IssueGraphNode) => ts(n.created_at) <= at;
  const closedBy = (n: IssueGraphNode) => {
    const c = ts(n.closed_at);
    return Number.isFinite(c) && c <= at ? n.closed_at : null;
  };

  // Pass 1: every node that existed, with its state on the day.
  const nodes: IssueGraphNode[] = [];
  const byNum = new Map<number, IssueGraphNode>();
  for (const n of data.nodes) {
    if (!exists(n)) continue;
    const closedAt = closedBy(n);
    const copy: IssueGraphNode = {
      ...n,
      state: closedAt ? "closed" : "open",
      closed_at: closedAt,
      updated_at: ts(n.updated_at) <= at ? n.updated_at : n.created_at,
      parent: n.parent !== null && live.has(n.parent) && exists(live.get(n.parent)!) ? n.parent : null,
      children: n.children.filter((c) => live.has(c) && exists(live.get(c)!)),
      pull_requests: [],
      descendants: { total: 0, done: 0, dropped: 0, open: 0 },
    };
    if (copy.kind === "pr") {
      copy.status = closedAt ? (n.status === "pr_merged" ? "pr_merged" : "pr_closed") : "pr_open";
      copy.issues = (n.issues ?? []).filter((i) => live.has(i) && exists(live.get(i)!));
    }
    nodes.push(copy);
    byNum.set(copy.number, copy);
  }

  // Pull requests as they stood: only those already opened, each with the
  // state it had on the day.
  for (const n of nodes) {
    if (n.kind !== "issue") continue;
    const src = live.get(n.number)!;
    n.pull_requests = src.pull_requests
      .map((p) => {
        const pr = byNum.get(p.number);
        if (!pr) return null;
        const state: "OPEN" | "MERGED" | "CLOSED" = pr.state === "open" ? "OPEN" : pr.status === "pr_merged" ? "MERGED" : "CLOSED";
        return { ...p, state };
      })
      .filter((p): p is NonNullable<typeof p> => p !== null);
  }

  // Pass 2: status, as the API derives it.
  const base = (n: IssueGraphNode): IssueGraphStatus => {
    if (n.state === "closed") return n.state_reason === "not_planned" || n.state_reason === "duplicate" ? "dropped" : "done";
    let merged = false;
    let open = false;
    for (const p of n.pull_requests) {
      if (p.state === "MERGED") merged = merged || p.closes === true;
      else if (p.state === "OPEN") open = true;
    }
    if (open) return "in_review";
    if (merged && n.children.length === 0) return "merged_open";
    return "open";
  };
  for (const n of nodes) if (n.kind === "issue") n.status = base(n);

  const edges: IssueGraphEdge[] = [];
  for (const e of data.edges) {
    if (byNum.has(e.from) && byNum.has(e.to)) edges.push(e);
  }
  for (const e of edges) {
    if (e.kind !== "depends_on") continue;
    const from = byNum.get(e.from)!;
    const to = byNum.get(e.to)!;
    if (from.kind === "issue" && from.status === "open" && to.state === "open") from.status = "blocked";
  }

  // Pass 3: progress down the primary tree, then "in progress" for a parent
  // with something finished under it.
  const progress = (num: number, visiting: Set<number>): IssueGraphNode["descendants"] => {
    const p = { total: 0, done: 0, dropped: 0, open: 0 };
    if (visiting.has(num)) return p;
    visiting.add(num);
    const n = byNum.get(num);
    if (!n) return p;
    for (const c of n.children) {
      const child = byNum.get(c);
      if (!child) continue;
      const cp = progress(c, visiting);
      p.total += 1 + cp.total;
      p.done += cp.done;
      p.dropped += cp.dropped;
      p.open += cp.open;
      if (child.status === "done") p.done++;
      else if (child.status === "dropped") p.dropped++;
      else p.open++;
    }
    return p;
  };
  for (const n of nodes) {
    if (n.kind !== "issue") continue;
    n.descendants = progress(n.number, new Set());
    if (n.status === "open" && n.children.length > 0 && n.descendants.done > 0) n.status = "in_progress";
  }

  // Pass 4: roots, orphans, stats.
  const stats = { ...data.stats, open: 0, closed: 0, done: 0, dropped: 0, blocked: 0, in_review: 0, merged_open: 0, epics: 0, orphans: 0, pull_requests: 0, open_pull_requests: 0 };
  const roots: number[] = [];
  const orphans: number[] = [];
  for (const n of nodes) {
    if (n.kind === "pr") {
      stats.pull_requests++;
      if (n.status === "pr_open") stats.open_pull_requests++;
      continue;
    }
    if (n.state === "open") stats.open++;
    else stats.closed++;
    switch (n.status) {
      case "done":
        stats.done++;
        break;
      case "dropped":
        stats.dropped++;
        break;
      case "blocked":
        stats.blocked++;
        break;
      case "in_review":
        stats.in_review++;
        break;
      case "merged_open":
        stats.merged_open++;
        break;
    }
    if (n.is_epic) stats.epics++;
    if (n.parent === null && n.children.length > 0) roots.push(n.number);
    if (n.parent === null && n.children.length === 0 && n.state === "open") orphans.push(n.number);
  }
  stats.orphans = orphans.length;
  roots.sort((a, b) => {
    const na = byNum.get(a)!;
    const nb = byNum.get(b)!;
    if ((na.state === "open") !== (nb.state === "open")) return na.state === "open" ? -1 : 1;
    return nb.updated_at.localeCompare(na.updated_at);
  });
  orphans.sort((a, b) => byNum.get(b)!.updated_at.localeCompare(byNum.get(a)!.updated_at));

  return { ...data, nodes, edges, roots, orphans, stats };
}

// Daily cumulative counts for the chart under the scrubber: issues opened,
// issues closed (done or dropped) and pull requests merged, one value per day
// from the first issue to the end of the range.
export type ActivitySeries = { days: number[]; opened: number[]; closed: number[]; merged: number[] };

export function activitySeries(data: IssueGraphResponse, range: TimeRange): ActivitySeries {
  const first = dayFloor(range.start);
  const last = dayFloor(range.end);
  const n = Math.max(1, Math.round((last - first) / DAY) + 1);
  const opened = new Array<number>(n).fill(0);
  const closed = new Array<number>(n).fill(0);
  const merged = new Array<number>(n).fill(0);
  const bucket = (iso: string | null | undefined) => {
    const t = ts(iso);
    if (!Number.isFinite(t)) return -1;
    return Math.min(n - 1, Math.max(0, Math.round((dayFloor(t) - first) / DAY)));
  };
  for (const node of data.nodes) {
    if (node.kind === "pr") {
      if (node.status === "pr_merged") {
        const b = bucket(node.closed_at);
        if (b >= 0) merged[b]++;
      }
      continue;
    }
    const o = bucket(node.created_at);
    if (o >= 0) opened[o]++;
    const c = bucket(node.closed_at);
    if (c >= 0) closed[c]++;
  }
  const days: number[] = [];
  for (let i = 0; i < n; i++) {
    days.push(first + i * DAY);
    if (i > 0) {
      opened[i] += opened[i - 1];
      closed[i] += closed[i - 1];
      merged[i] += merged[i - 1];
    }
  }
  return { days, opened, closed, merged };
}

export function formatDay(ms: number): string {
  return new Date(ms).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}
