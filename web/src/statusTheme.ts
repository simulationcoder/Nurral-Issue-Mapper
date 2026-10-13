import type { IssueGraphEdgeKind, IssueGraphNode, IssueGraphStatus } from "./types";

// Three node colours, one per kind of thing: an open issue, a closed issue, a
// pull request. That is the first question anyone asks of the map, so it is the
// colour of the node itself (accent bar, tint, minimap dot). Everything finer --
// blocked, in review, merged but not closed -- is a pill inside the node.
export const KIND_COLOR = {
  open: "#0ea5e9", // sky: open issue
  closed: "#10b981", // emerald: closed issue
  dropped: "#94a3b8", // slate: closed as not planned / duplicate
  pr: "#8b5cf6", // violet: pull request
} as const;

export type Accent = { color: string; label: string };

export function accentOf(node: Pick<IssueGraphNode, "kind" | "state" | "status">): Accent {
  if (node.kind === "pr") return { color: KIND_COLOR.pr, label: "Pull request" };
  if (node.state === "open") return { color: KIND_COLOR.open, label: "Open issue" };
  if (node.status === "dropped") return { color: KIND_COLOR.dropped, label: "Closed, not planned" };
  return { color: KIND_COLOR.closed, label: "Closed issue" };
}

// The pill. Its colour is the finer reading; for a plain open or done issue it
// repeats the kind colour so the pill never contradicts the bar.
export const STATUS: Record<IssueGraphStatus, { label: string; short: string; color: string; order: number }> = {
  blocked: { label: "Blocked", short: "Blocked", color: "#f43f5e", order: 0 },
  in_review: { label: "In review", short: "PR open", color: "#8b5cf6", order: 1 },
  in_progress: { label: "In progress", short: "In progress", color: "#f59e0b", order: 2 },
  // A merged PR names this issue but the issue is still open: usually finished
  // work nobody closed, sometimes a PR that only mentioned it. A hint, not a verdict.
  merged_open: { label: "PR merged, still open", short: "PR merged", color: "#14b8a6", order: 3 },
  open: { label: "Open", short: "Open", color: KIND_COLOR.open, order: 4 },
  done: { label: "Done", short: "Done", color: KIND_COLOR.closed, order: 5 },
  dropped: { label: "Not planned", short: "Dropped", color: KIND_COLOR.dropped, order: 6 },
  pr_open: { label: "PR open", short: "Open PR", color: KIND_COLOR.pr, order: 7 },
  pr_merged: { label: "PR merged", short: "Merged", color: KIND_COLOR.pr, order: 8 },
  pr_closed: { label: "PR closed without merge", short: "Closed", color: KIND_COLOR.dropped, order: 9 },
};

export const ISSUE_STATUS_ORDER: IssueGraphStatus[] = [
  "blocked",
  "in_review",
  "in_progress",
  "merged_open",
  "open",
  "done",
  "dropped",
];

export function statusOf(status: string) {
  return STATUS[(status as IssueGraphStatus) in STATUS ? (status as IssueGraphStatus) : "open"];
}

export function isClosed(node: Pick<IssueGraphNode, "state">) {
  return node.state === "closed";
}

// 12% alpha on a hue reads as a tint in both themes.
export function tint(hex: string, alpha = "1f") {
  return `${hex}${alpha}`;
}

export const EDGE_STYLE: Record<
  IssueGraphEdgeKind,
  { label: string; color: string; dash?: string; arrow: boolean; description: string }
> = {
  parent: { label: "sub-issue", color: "#64748b", arrow: true, description: "GitHub sub-issue" },
  part_of: { label: "part of", color: "#64748b", arrow: true, description: "“Part of #N” in the body" },
  series: { label: "series", color: "#64748b", dash: "6 4", arrow: true, description: "Numbered title (3/8) opened with the epic" },
  follow_up: { label: "follow-up", color: "#a855f7", dash: "2 4", arrow: true, description: "Follow-up to, found via" },
  context: { label: "mentions", color: "#94a3b8", dash: "2 4", arrow: true, description: "First issue the body mentions" },
  depends_on: { label: "blocks", color: "#f59e0b", dash: "7 4", arrow: true, description: "Depends on, blocked by" },
  supersedes: { label: "supersedes", color: "#f43f5e", dash: "2 3", arrow: true, description: "Replaces, closes, duplicate of" },
  related: { label: "related", color: "#cbd5e1", dash: "1 4", arrow: false, description: "Related, see also" },
  closes: { label: "closes", color: KIND_COLOR.pr, arrow: true, description: "Pull request closes the issue" },
  references: { label: "refs", color: KIND_COLOR.pr, dash: "3 3", arrow: true, description: "Pull request mentions the issue" },
};

export function ago(iso: string | null | undefined): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  const minutes = Math.floor(ms / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 45) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}
