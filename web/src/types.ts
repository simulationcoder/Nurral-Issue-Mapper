// Mirrors graph.Graph (GET /api/graph). Issues and pull
// requests are both nodes; `kind` tells them apart. `status` is the API's
// derived reading; `state` is GitHub's open/closed.
export type IssueGraphStatus =
  | "open"
  | "in_progress"
  | "blocked"
  | "in_review"
  | "merged_open"
  | "done"
  | "dropped"
  | "pr_open"
  | "pr_merged"
  | "pr_closed";

export type IssueGraphEdgeKind =
  | "parent"
  | "part_of"
  | "series"
  | "follow_up"
  | "context"
  | "depends_on"
  | "supersedes"
  | "related"
  | "closes"
  | "references";

export type IssueGraphLabel = { name: string; color: string };

export type IssueGraphPR = { number: number; state: "OPEN" | "MERGED" | "CLOSED"; url?: string; closes: boolean };

export type IssueGraphProgress = { total: number; done: number; dropped: number; open: number };

export type IssueGraphNode = {
  kind: "issue" | "pr";
  number: number;
  title: string;
  state: "open" | "closed";
  state_reason: string;
  status: IssueGraphStatus;
  url: string;
  created_at: string;
  updated_at: string;
  closed_at: string | null;
  labels: IssueGraphLabel[];
  is_epic: boolean;
  parent: number | null;
  parent_kind?: IssueGraphEdgeKind;
  parent_inferred?: boolean;
  children: number[];
  descendants: IssueGraphProgress;
  pull_requests: IssueGraphPR[];
  issues?: number[];
  series?: string;
  excerpt: string;
  depth: number;
};

export type IssueGraphEdge = {
  from: number;
  to: number;
  kind: IssueGraphEdgeKind;
  inferred?: boolean;
  primary?: boolean;
};

export type IssueGraphStats = {
  open: number;
  closed: number;
  done: number;
  dropped: number;
  blocked: number;
  in_review: number;
  merged_open: number;
  epics: number;
  orphans: number;
  pull_requests: number;
  open_pull_requests: number;
};

export type IssueGraphResponse = {
  repo: string;
  fetched_at: string;
  cached: boolean;
  ttl_seconds: number;
  truncated?: boolean;
  nodes: IssueGraphNode[];
  edges: IssueGraphEdge[];
  roots: number[];
  orphans: number[];
  stats: IssueGraphStats;
};
