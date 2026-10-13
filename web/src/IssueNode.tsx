import { memo } from "react";
import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { GitMerge, GitPullRequestArrow, GitPullRequestClosed, Layers, Link2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { IssueGraphNode } from "./types";
import type { LayoutDirection } from "./layout";
import { accentOf, statusOf, tint } from "./statusTheme";

export type IssueNodeData = {
  issue: IssueGraphNode;
  direction: LayoutDirection;
  // A ghost is an issue outside the current tree, drawn because something in
  // the tree depends on it.
  ghost?: boolean;
  isRoot?: boolean;
  // Set for the moment a card lingers after leaving the layout, so it can fade.
  exiting?: boolean;
};

export type IssueFlowNode = Node<IssueNodeData, "issue">;

export const NODE_WIDTH = 292;
export const PR_NODE_WIDTH = 252;

export function nodeHeight(issue: IssueGraphNode, isRoot: boolean): number {
  if (issue.kind === "pr") return 68;
  let h = 90;
  if (issue.children.length > 0) h += 20;
  if (issue.labels.length > 0) h += 20;
  if (isRoot) h += 6;
  return h;
}

function IssueNodeComponent({ data, selected }: NodeProps<IssueFlowNode>) {
  const { issue, direction, ghost, isRoot, exiting } = data;
  const accent = accentOf(issue);
  const s = statusOf(issue.status);
  const isPR = issue.kind === "pr";
  const closed = issue.state === "closed";
  const d = issue.descendants;
  const doneShare = d.total ? (d.done / d.total) * 100 : 0;
  const droppedShare = d.total ? (d.dropped / d.total) * 100 : 0;
  const merged = issue.pull_requests.some((p) => p.state === "MERGED" && p.closes);
  const prOpen = issue.pull_requests.some((p) => p.state === "OPEN");

  const PrIcon = issue.status === "pr_merged" ? GitMerge : issue.status === "pr_closed" ? GitPullRequestClosed : GitPullRequestArrow;

  return (
    <div
      className={cn(
        "issue-card relative border text-card-foreground shadow-sm",
        isPR ? "rounded-lg" : "rounded-xl",
        isRoot ? "border-foreground/30 shadow-md" : "border-border",
        ghost && "border-dashed opacity-80",
        selected && "shadow-lg",
        exiting && "issue-card-exit"
      )}
      style={{
        width: isPR ? PR_NODE_WIDTH : NODE_WIDTH,
        borderLeft: `4px solid ${accent.color}`,
        // The tint is the kind colour at low alpha over the card, so open,
        // closed and PR nodes read apart at a glance, even zoomed out. It is
        // the background colour, with the card colour laid over its right
        // side, so a change of kind (an issue closing in playback) can ease
        // from sky to emerald: a gradient on its own cannot be transitioned.
        backgroundColor: tint(accent.color, closed && !isPR ? "16" : "24"),
        backgroundImage: "linear-gradient(90deg, hsl(var(--card) / 0) 0%, hsl(var(--card) / 0.6) 45%, hsl(var(--card)) 100%)",
        boxShadow: selected ? `0 0 0 2px ${accent.color}, 0 8px 24px -8px ${accent.color}66` : undefined,
      }}
    >
      <Handle
        type="target"
        position={direction === "TB" ? Position.Top : Position.Left}
        className="!h-2 !w-2 !border-0 !bg-transparent"
      />
      {isPR ? (
        <div className="px-3 pb-2 pt-2">
          <div className="flex items-center gap-1.5">
            <PrIcon className="h-3.5 w-3.5" style={{ color: accent.color }} aria-label="pull request" />
            <span className="text-[11.5px] font-semibold tracking-tight" style={{ color: accent.color }}>
              PR #{issue.number}
            </span>
            <span
              className="issue-pill ml-auto inline-flex items-center rounded-full px-2 py-[2px] text-[10px] font-semibold leading-none"
              style={{ background: tint(s.color), color: s.color }}
            >
              {s.short}
            </span>
          </div>
          <div className="mt-0.5 line-clamp-1 text-[13px] leading-snug text-foreground/90" title={issue.title}>
            {issue.title}
          </div>
        </div>
      ) : (
        <div className="px-3.5 pb-3 pt-3">
          <div className="flex items-center gap-1.5">
            <span className="issue-title text-[13px] font-semibold tracking-tight" style={{ color: accent.color }}>
              #{issue.number}
            </span>
            {issue.is_epic && <Layers className="h-3 w-3 text-muted-foreground" aria-label="epic" />}
            {ghost && <Link2 className="h-3 w-3 text-muted-foreground" aria-label="outside this tree" />}
            <span
              className="issue-pill ml-auto inline-flex items-center rounded-full px-2 py-[2px] text-[10px] font-semibold leading-none"
              style={{ background: tint(s.color), color: s.color }}
            >
              {s.short}
            </span>
            {merged && <GitMerge className="h-3.5 w-3.5" style={{ color: "#14b8a6" }} aria-label="PR merged" />}
            {!merged && prOpen && (
              <GitPullRequestArrow className="h-3.5 w-3.5" style={{ color: "#8b5cf6" }} aria-label="PR open" />
            )}
          </div>
          <div
            className={cn("issue-title mt-1.5 line-clamp-2 text-[13.5px] font-medium leading-snug", closed && "text-foreground/70")}
            title={issue.title}
          >
            {issue.title}
          </div>
          {issue.children.length > 0 && (
            <div className="mt-2 flex items-center gap-2">
              <div className="flex h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                <div className="h-full" style={{ width: `${doneShare}%`, background: "#10b981" }} />
                <div className="h-full" style={{ width: `${droppedShare}%`, background: "#94a3b8" }} />
              </div>
              <span className="whitespace-nowrap text-[10.5px] font-medium text-muted-foreground">
                {d.done}/{d.total} done
              </span>
            </div>
          )}
          {issue.labels.length > 0 && (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {issue.labels.slice(0, 3).map((l) => (
                <span
                  key={l.name}
                  className="inline-flex items-center gap-1 rounded-md px-1.5 py-[1px] text-[10px] text-muted-foreground"
                  style={{ background: `#${l.color}22` }}
                >
                  <span className="h-1.5 w-1.5 rounded-full" style={{ background: `#${l.color}` }} />
                  {l.name}
                </span>
              ))}
            </div>
          )}
        </div>
      )}
      <Handle
        type="source"
        position={direction === "TB" ? Position.Bottom : Position.Right}
        className="!h-2 !w-2 !border-0 !bg-transparent"
      />
    </div>
  );
}

export const IssueNode = memo(IssueNodeComponent);
