import type { IssueGraphEdge, IssueGraphEdgeKind, IssueGraphNode, IssueGraphResponse } from "./types";
import type { IssueIndex } from "./useIssueGraph";

export type View = "all" | "orphans" | number;

export type SelectionOptions = {
  view: View;
  hideDone: boolean;
  showRelated: boolean;
  showPRs: boolean;
};

export type GraphSelection = {
  rootNumber: number | null;
  // Issues on the canvas.
  shown: Set<number>;
  // Open issues outside the tree that something in it depends on.
  ghosts: Set<number>;
  prs: IssueGraphNode[];
  // Edges, all read "from -> to" as the API gives them.
  hierarchy: IssueGraphEdge[];
  overlay: IssueGraphEdge[];
  prEdges: IssueGraphEdge[];
};

// Secondary relations drawn over the tree; these kinds never shape a layout.
const OVERLAY_KINDS: IssueGraphEdgeKind[] = ["depends_on", "follow_up", "supersedes", "related", "context", "series", "part_of", "parent"];

// Which issues, PRs and edges a view shows. Shared by the 2D tree and the 3D
// view so a filter means the same thing in both.
export function selectGraph(data: IssueGraphResponse, index: IssueIndex, opts: SelectionOptions): GraphSelection {
  let shown = new Set<number>();
  let rootNumber: number | null = null;
  if (opts.view === "all") {
    for (const n of data.nodes) if (n.kind === "issue") shown.add(n.number);
  } else if (opts.view === "orphans") {
    for (const n of data.orphans) shown.add(n);
  } else {
    rootNumber = opts.view;
    const queue = [opts.view];
    while (queue.length) {
      const cur = queue.shift()!;
      if (shown.has(cur)) continue;
      const nd = index.get(cur);
      // Absent on a playback day before the issue was opened.
      if (!nd) continue;
      shown.add(cur);
      queue.push(...nd.children);
    }
  }

  if (opts.hideDone) {
    // Keep a closed issue only while something open still hangs under it.
    shown = new Set(
      [...shown].filter((num) => {
        const nd = index.get(num);
        if (!nd) return false;
        if (num === rootNumber) return true;
        return nd.state === "open" || nd.descendants.open > 0;
      })
    );
  }

  const ghosts = new Set<number>();
  if (opts.view !== "all") {
    for (const e of data.edges) {
      if (e.kind === "depends_on" && shown.has(e.from) && !shown.has(e.to)) {
        const t = index.get(e.to);
        if (t && t.state === "open") ghosts.add(e.to);
      }
    }
  }

  // PRs attached to anything shown: the ones that close an issue, and any
  // still open. A merged PR that only mentions an issue in passing is noise
  // (there are hundreds) and comes in with the mention edges.
  const prs: IssueGraphNode[] = [];
  const prEdges: IssueGraphEdge[] = [];
  if (opts.showPRs) {
    for (const e of data.edges) {
      if ((e.kind === "closes" || e.kind === "references") && shown.has(e.to)) {
        const pr = index.get(e.from);
        if (!pr) continue;
        if (e.kind === "references" && pr.state === "closed" && !opts.showRelated) continue;
        if (opts.hideDone && pr.state === "closed") {
          const issue = index.get(e.to);
          if (issue && issue.state === "closed") continue;
        }
        prEdges.push(e);
      }
    }
    const seen = new Set<number>();
    for (const e of prEdges) {
      if (seen.has(e.from)) continue;
      seen.add(e.from);
      prs.push(index.get(e.from)!);
    }
  }

  const hierarchy: IssueGraphEdge[] = [];
  const overlay: IssueGraphEdge[] = [];
  const primaryPairs = new Set<string>();
  for (const e of data.edges) if (e.primary) primaryPairs.add(`${e.from}>${e.to}`);
  for (const e of data.edges) {
    if (e.kind === "closes" || e.kind === "references") continue;
    const bothShown = (shown.has(e.from) || ghosts.has(e.from)) && (shown.has(e.to) || ghosts.has(e.to));
    if (!bothShown) continue;
    if (e.primary) {
      hierarchy.push(e);
      continue;
    }
    if (!OVERLAY_KINDS.includes(e.kind)) continue;
    // An issue that is both a native sub-issue and says "Part of #N" has two
    // edges to the same parent; one line is enough.
    if (["parent", "part_of", "series", "context"].includes(e.kind) && primaryPairs.has(`${e.from}>${e.to}`)) continue;
    if (e.kind === "related" && !opts.showRelated) continue;
    overlay.push(e);
  }

  return { rootNumber, shown, ghosts, prs, hierarchy, overlay, prEdges };
}

export function parentChain(num: number, index: IssueIndex): IssueGraphNode[] {
  const out: IssueGraphNode[] = [];
  let cur = index.get(num);
  const seen = new Set<number>();
  while (cur && cur.parent !== null && !seen.has(cur.number)) {
    seen.add(cur.number);
    const p = index.get(cur.parent);
    if (!p) break;
    out.unshift(p);
    cur = p;
  }
  return out;
}
