import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  getNodesBounds,
  getViewportForBounds,
  useReactFlow,
  type Edge,
  type NodeMouseHandler,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  AlertCircle,
  ArrowDownToLine,
  ArrowRightToLine,
  ChevronRight,
  Crosshair,
  ExternalLink,
  GitFork,
  GitPullRequestArrow,
  History,
  Layers,
  Maximize2,
  Minimize2,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  RefreshCw,
  Search,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useTheme } from "./theme";
import type { IssueGraphEdge, IssueGraphEdgeKind, IssueGraphNode, IssueGraphResponse } from "./types";
import { CurvedEdge } from "./CurvedEdge";
import { IssueNode, NODE_WIDTH, PR_NODE_WIDTH, nodeHeight, type IssueFlowNode } from "./IssueNode";
import { layoutGraph, type LayoutDirection, type LayoutInput, type Positioned } from "./layout";
import { tweenViewport, useTweenedNodes } from "./motion";
import { parentChain, selectGraph, type GraphSelection, type View } from "./selection";
import { EDGE_STYLE, ISSUE_STATUS_ORDER, KIND_COLOR, STATUS, accentOf, ago, statusOf, tint } from "./statusTheme";
import { SPEEDS, Timeline } from "./Timeline";
import { activitySeries, dayFloor, formatDay, graphAsOf, timelineRange } from "./timeline";
import { useIssueGraph, type IssueIndex } from "./useIssueGraph";

const nodeTypes = { issue: IssueNode };
const edgeTypes = { curved: CurvedEdge };

// How long a relayout and its camera move take.
const MOVE_MS = 650;

function flowEdge(e: IssueGraphEdge, opts: { primary?: boolean; label?: boolean; animated?: boolean }): Edge {
  const st = EDGE_STYLE[e.kind];
  let source = String(e.to);
  let target = String(e.from);
  // "from depends on to" is drawn blocker -> dependent; "from supersedes to"
  // is drawn new -> old; a PR edge is drawn issue -> PR so the PR hangs below.
  if (e.kind === "supersedes") {
    source = String(e.from);
    target = String(e.to);
  }
  const dashed = e.inferred ? "5 4" : st.dash;
  return {
    id: `${e.kind}:${e.from}>${e.to}`,
    source,
    target,
    type: "curved",
    className: "issue-edge",
    animated: opts.animated,
    style: {
      stroke: st.color,
      strokeWidth: opts.primary ? 1.6 : 1.2,
      strokeDasharray: dashed,
      opacity: e.kind === "related" ? 0.7 : 1,
    },
    markerEnd: st.arrow ? { type: MarkerType.ArrowClosed, color: st.color, width: 14, height: 14 } : undefined,
    label: opts.label ? (e.inferred && e.kind !== "series" && e.kind !== "context" ? `${st.label} (inferred)` : st.label) : undefined,
    labelStyle: { fontSize: 10, fontWeight: 600, fill: st.color },
    labelBgStyle: { fill: "hsl(var(--card))", fillOpacity: 0.95 },
    labelBgPadding: [5, 2],
    labelBgBorderRadius: 4,
    zIndex: opts.primary ? 0 : 1,
  };
}

function layoutInputs(sel: GraphSelection, index: IssueIndex): LayoutInput[] {
  return [
    ...[...sel.shown].map((num) => {
      const nd = index.get(num)!;
      return { id: String(num), width: NODE_WIDTH, height: nodeHeight(nd, num === sel.rootNumber) };
    }),
    ...[...sel.ghosts].map((num) => ({ id: String(num), width: NODE_WIDTH, height: nodeHeight(index.get(num)!, false) })),
    ...sel.prs.map((pr) => ({ id: String(pr.number), width: PR_NODE_WIDTH, height: nodeHeight(pr, false) })),
  ];
}

// Where every node of a selection goes. Kept apart from the nodes themselves
// so that playback, which shows a past subset of today's selection, can keep
// today's layout and reveal cards in place instead of reshuffling the tree on
// every day that passes.
function layoutPositions(sel: GraphSelection, index: IssueIndex, direction: LayoutDirection): Positioned {
  const layoutNodes = layoutInputs(sel, index);
  // PRs sit one rank below their first issue.
  const prLayoutEdges: { source: string; target: string }[] = [];
  const prPlaced = new Set<number>();
  for (const e of sel.prEdges) {
    if (prPlaced.has(e.from)) continue;
    prPlaced.add(e.from);
    prLayoutEdges.push({ source: String(e.to), target: String(e.from) });
  }
  return layoutGraph(
    layoutNodes,
    [...sel.hierarchy.map((e) => ({ source: String(e.to), target: String(e.from) })), ...prLayoutEdges],
    direction
  );
}

// Build the React Flow nodes and edges of a selection on given positions. A
// node the positions do not know (an issue that was an orphan then and is
// parented now, say) is set beside a neighbour, or in a row under the mosaic.
function buildFlow(sel: GraphSelection, index: IssueIndex, direction: LayoutDirection, positions: Positioned) {
  const layoutNodes = layoutInputs(sel, index);
  const placed = new Map<string, { x: number; y: number }>();
  let maxY = 0;
  for (const ln of layoutNodes) {
    const p = positions.get(ln.id);
    if (!p) continue;
    placed.set(ln.id, p);
    maxY = Math.max(maxY, p.y + ln.height);
  }
  let spareX = 0;
  for (const ln of layoutNodes) {
    if (placed.has(ln.id)) continue;
    const num = Number(ln.id);
    const neighbour = [...sel.hierarchy, ...sel.overlay, ...sel.prEdges].find(
      (e) => (e.from === num && placed.has(String(e.to))) || (e.to === num && placed.has(String(e.from)))
    );
    if (neighbour) {
      const other = placed.get(String(neighbour.from === num ? neighbour.to : neighbour.from))!;
      placed.set(ln.id, direction === "LR" ? { x: other.x + NODE_WIDTH + 160, y: other.y } : { x: other.x, y: other.y + 200 });
    } else {
      placed.set(ln.id, { x: spareX, y: maxY + 120 });
      spareX += ln.width + 40;
    }
  }
  const nodes: IssueFlowNode[] = layoutNodes.map((ln) => {
    const num = Number(ln.id);
    const nd = index.get(num)!;
    return {
      id: ln.id,
      type: "issue",
      position: placed.get(ln.id)!,
      data: { issue: nd, direction, ghost: sel.ghosts.has(num), isRoot: num === sel.rootNumber },
      width: ln.width,
      height: ln.height,
      draggable: false,
      connectable: false,
    };
  });
  // Labels only where they change the reading: an inferred parent and a
  // blocker. Everything else is told by colour and dash, and a label on every
  // edge of a ten-child column lands on the nodes.
  const edges: Edge[] = [
    ...sel.hierarchy.map((e) => flowEdge(e, { primary: true, label: !!e.inferred })),
    ...sel.overlay.map((e) => {
      const blockerOpen = e.kind === "depends_on" && index.get(e.to)?.state === "open" && index.get(e.from)?.state === "open";
      return flowEdge(e, { label: e.kind === "depends_on", animated: blockerOpen });
    }),
    ...sel.prEdges.map((e) => flowEdge(e, { label: e.kind === "references" })),
  ];
  return { nodes, edges };
}

function ProgressBar({ node, className }: { node: IssueGraphNode; className?: string }) {
  const d = node.descendants;
  if (!d.total) return null;
  return (
    <div className={cn("flex h-1.5 w-full overflow-hidden rounded-full bg-muted", className)}>
      <div className="h-full" style={{ width: `${(d.done / d.total) * 100}%`, background: KIND_COLOR.closed }} />
      <div className="h-full" style={{ width: `${(d.dropped / d.total) * 100}%`, background: KIND_COLOR.dropped }} />
      <div className="h-full" style={{ width: `${(d.open / d.total) * 100}%`, background: tint(KIND_COLOR.open, "55") }} />
    </div>
  );
}

function StatusPill({ node }: { node: IssueGraphNode }) {
  const s = statusOf(node.status);
  return (
    <span
      className="inline-flex items-center rounded-full px-2 py-[2px] text-[10.5px] font-semibold leading-none"
      style={{ background: tint(s.color), color: s.color }}
    >
      {s.label}
    </span>
  );
}

function StatTile({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border bg-card px-2.5 py-1.5">
      <span className="h-2 w-2 rounded-full" style={{ background: color }} />
      <div className="leading-tight">
        <div className="text-[14px] font-semibold tabular-nums">{value}</div>
        <div className="text-[9.5px] uppercase tracking-wide text-muted-foreground">{label}</div>
      </div>
    </div>
  );
}

function IssueRow({ node, onSelect, active }: { node: IssueGraphNode; onSelect: (n: number) => void; active?: boolean }) {
  const accent = accentOf(node);
  return (
    <button
      type="button"
      onClick={() => onSelect(node.number)}
      className={cn(
        "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-[12px] transition-colors hover:bg-secondary/60",
        active && "bg-secondary"
      )}
    >
      <span className="mt-[5px] h-2 w-2 shrink-0 rounded-full" style={{ background: accent.color }} />
      <span className="font-semibold" style={{ color: accent.color }}>
        {node.kind === "pr" ? "PR " : ""}#{node.number}
      </span>
      <span className={cn("line-clamp-1 flex-1", node.state === "closed" && "text-muted-foreground")}>{node.title}</span>
    </button>
  );
}

function DetailPanel({
  node,
  index,
  edges,
  onSelect,
  onFocus,
  onClose,
}: {
  node: IssueGraphNode;
  index: IssueIndex;
  edges: IssueGraphEdge[];
  onSelect: (n: number) => void;
  onFocus: (n: number) => void;
  onClose: () => void;
}) {
  const accent = accentOf(node);
  const chain = parentChain(node.number, index);
  const rel = useMemo(() => {
    const out = {
      dependsOn: [] as IssueGraphNode[],
      blocks: [] as IssueGraphNode[],
      followUps: [] as IssueGraphNode[],
      related: [] as IssueGraphNode[],
      supersedes: [] as IssueGraphNode[],
      prs: [] as IssueGraphNode[],
      issues: [] as IssueGraphNode[],
    };
    for (const e of edges) {
      if (e.from === node.number) {
        const t = index.get(e.to);
        if (!t) continue;
        if (e.kind === "depends_on") out.dependsOn.push(t);
        else if (e.kind === "related") out.related.push(t);
        else if (e.kind === "supersedes") out.supersedes.push(t);
        else if (e.kind === "closes" || e.kind === "references") out.issues.push(t);
      } else if (e.to === node.number) {
        const f = index.get(e.from);
        if (!f) continue;
        if (e.kind === "depends_on") out.blocks.push(f);
        else if (e.kind === "follow_up" && !e.primary) out.followUps.push(f);
        else if (e.kind === "related") out.related.push(f);
        else if (e.kind === "closes" || e.kind === "references") out.prs.push(f);
      }
    }
    return out;
  }, [edges, index, node.number]);

  const children = node.children.map((c) => index.get(c)).filter((c): c is IssueGraphNode => !!c);
  const grouped = ISSUE_STATUS_ORDER.map((s) => ({ status: s, items: children.filter((c) => c.status === s) })).filter(
    (g) => g.items.length
  );

  const Section = ({ title, items }: { title: string; items: IssueGraphNode[] }) =>
    items.length ? (
      <div>
        <div className="mb-1 text-[10.5px] font-semibold uppercase tracking-wide text-muted-foreground">{title}</div>
        <div className="space-y-0.5">
          {items.map((it) => (
            <IssueRow key={`${title}-${it.number}`} node={it} onSelect={onSelect} />
          ))}
        </div>
      </div>
    ) : null;

  return (
    <aside className="flex h-full w-[340px] shrink-0 flex-col border-l bg-background">
      <div className="flex items-start gap-2 border-b px-4 py-3" style={{ borderTop: `3px solid ${accent.color}` }}>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-[12px] font-semibold" style={{ color: accent.color }}>
              {node.kind === "pr" ? "PR " : ""}#{node.number}
            </span>
            <span className="text-[11px] text-muted-foreground">{accent.label}</span>
            {node.is_epic && <Layers className="h-3 w-3 text-muted-foreground" />}
          </div>
          <h3 className="mt-1 text-[14px] font-semibold leading-snug">{node.title}</h3>
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <StatusPill node={node} />
            {node.state_reason && node.state_reason !== "completed" && (
              <span className="text-[10.5px] text-muted-foreground">{node.state_reason.replace("_", " ")}</span>
            )}
            {node.series && <span className="text-[10.5px] text-muted-foreground">series: {node.series}</span>}
          </div>
        </div>
        <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={onClose} aria-label="Close details">
          <X className="h-4 w-4" />
        </Button>
      </div>

      <div className="flex-1 space-y-4 overflow-y-auto px-4 py-3 text-[12px]">
        <div className="grid grid-cols-3 gap-2 text-[11px]">
          <div>
            <div className="text-muted-foreground">Opened</div>
            <div className="font-medium">{ago(node.created_at)}</div>
          </div>
          <div>
            <div className="text-muted-foreground">Updated</div>
            <div className="font-medium">{ago(node.updated_at)}</div>
          </div>
          <div>
            <div className="text-muted-foreground">{node.kind === "pr" ? "Merged / closed" : "Closed"}</div>
            <div className="font-medium">{node.closed_at ? ago(node.closed_at) : "—"}</div>
          </div>
        </div>

        <div className="flex gap-2">
          <Button asChild size="sm" variant="outline" className="h-7 gap-1.5 text-[11.5px]">
            <a href={node.url} target="_blank" rel="noopener noreferrer">
              <ExternalLink className="h-3.5 w-3.5" /> Open on GitHub
            </a>
          </Button>
          {node.children.length > 0 && (
            <Button size="sm" variant="secondary" className="h-7 gap-1.5 text-[11.5px]" onClick={() => onFocus(node.number)}>
              <Crosshair className="h-3.5 w-3.5" /> Focus this tree
            </Button>
          )}
        </div>

        {chain.length > 0 && (
          <div>
            <div className="mb-1 text-[10.5px] font-semibold uppercase tracking-wide text-muted-foreground">Belongs to</div>
            <div className="flex flex-wrap items-center gap-1 text-[11.5px]">
              {chain.map((p, i) => (
                <span key={p.number} className="flex items-center gap-1">
                  {i > 0 && <ChevronRight className="h-3 w-3 text-muted-foreground" />}
                  <button
                    type="button"
                    className="rounded px-1 font-medium hover:bg-secondary"
                    style={{ color: accentOf(p).color }}
                    onClick={() => onSelect(p.number)}
                    title={p.title}
                  >
                    #{p.number}
                  </button>
                </span>
              ))}
              {node.parent_kind && (
                <span className="ml-1 text-[10.5px] text-muted-foreground">
                  via {EDGE_STYLE[node.parent_kind].label}
                  {node.parent_inferred ? ", inferred" : ""}
                </span>
              )}
            </div>
          </div>
        )}

        {node.descendants.total > 0 && (
          <div>
            <div className="mb-1 flex items-center justify-between text-[10.5px] font-semibold uppercase tracking-wide text-muted-foreground">
              <span>Progress</span>
              <span className="font-medium normal-case tracking-normal">
                {node.descendants.done} done · {node.descendants.dropped} dropped · {node.descendants.open} open
              </span>
            </div>
            <ProgressBar node={node} />
          </div>
        )}

        {grouped.map((g) => (
          <Section key={g.status} title={`${STATUS[g.status].label} (${g.items.length})`} items={g.items} />
        ))}

        <Section title="Blocked by" items={rel.dependsOn} />
        <Section title="Blocks" items={rel.blocks} />
        <Section title="Follow-ups" items={rel.followUps} />
        <Section title="Supersedes" items={rel.supersedes} />
        <Section title={node.kind === "pr" ? "Closes or references" : "Pull requests"} items={node.kind === "pr" ? rel.issues : rel.prs} />
        <Section title="Related" items={rel.related} />

        {node.labels.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {node.labels.map((l) => (
              <span
                key={l.name}
                className="inline-flex items-center gap-1 rounded-md px-1.5 py-[2px] text-[10.5px]"
                style={{ background: `#${l.color}22`, color: "inherit" }}
              >
                <span className="h-1.5 w-1.5 rounded-full" style={{ background: `#${l.color}` }} />
                {l.name}
              </span>
            ))}
          </div>
        )}

        {node.excerpt && (
          <div>
            <div className="mb-1 text-[10.5px] font-semibold uppercase tracking-wide text-muted-foreground">Description</div>
            <p className="whitespace-pre-wrap text-[11.5px] leading-relaxed text-foreground/80">{node.excerpt}</p>
          </div>
        )}
      </div>
    </aside>
  );
}

function Legend() {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[10.5px] text-muted-foreground">
      <span className="flex items-center gap-1.5">
        <span className="h-2.5 w-2.5 rounded-sm" style={{ background: KIND_COLOR.open }} /> Open issue
      </span>
      <span className="flex items-center gap-1.5">
        <span className="h-2.5 w-2.5 rounded-sm" style={{ background: KIND_COLOR.closed }} /> Closed issue
      </span>
      <span className="flex items-center gap-1.5">
        <span className="h-2.5 w-2.5 rounded-sm" style={{ background: KIND_COLOR.dropped }} /> Not planned
      </span>
      <span className="flex items-center gap-1.5">
        <span className="h-2.5 w-2.5 rounded-sm" style={{ background: KIND_COLOR.pr }} /> Pull request
      </span>
      <span className="mx-1 h-3 w-px bg-border" />
      {(["part_of", "series", "follow_up", "depends_on", "closes", "related"] as IssueGraphEdgeKind[]).map((k) => (
        <span key={k} className="flex items-center gap-1.5">
          <svg width="26" height="6" aria-hidden>
            <line x1="0" y1="3" x2="26" y2="3" stroke={EDGE_STYLE[k].color} strokeWidth="1.6" strokeDasharray={EDGE_STYLE[k].dash} />
          </svg>
          {EDGE_STYLE[k].label}
        </span>
      ))}
      <span className="flex items-center gap-1.5">
        <svg width="26" height="6" aria-hidden>
          <line x1="0" y1="3" x2="26" y2="3" stroke="#64748b" strokeWidth="1.6" strokeDasharray="5 4" />
        </svg>
        dashed = inferred
      </span>
    </div>
  );
}

function Canvas({
  nodes,
  edges,
  selectedId,
  onSelect,
  onFocus,
  colorMode,
  fitKey,
  followKey,
  centerOn,
  fitMinZoom,
}: {
  nodes: IssueFlowNode[];
  edges: Edge[];
  selectedId: number | null;
  onSelect: (n: number | null) => void;
  onFocus: (n: number) => void;
  colorMode: "light" | "dark";
  fitKey: string;
  // Changes on every day of playback, so the camera keeps the growing tracker
  // in frame; null when the map is not playing.
  followKey: number | null;
  centerOn: number | null;
  // The zoom a fit may not go below. For one epic the text must stay legible
  // even if the column overflows; for the whole tracker the overview wins.
  fitMinZoom?: number;
}) {
  const rf = useReactFlow();
  const wrap = useRef<HTMLDivElement>(null);
  // One React Flow instance for the life of the tab: cards keep their identity
  // across a change of view, so the ones in both layouts glide to their new
  // place while the camera eases to the new framing. Viewport moves are
  // animated by hand (motion.ts) rather than with React Flow's `duration`,
  // which goes through d3-transition and depends on the bundler resolving one
  // d3-selection (see vite.config.ts).
  const displayNodes = useTweenedNodes(nodes, MOVE_MS);

  const mounted = useRef(false);
  useEffect(() => {
    if (!mounted.current) {
      // The first framing is the fitView prop's, once the nodes are measured.
      mounted.current = true;
      return;
    }
    let cancel: () => void = () => undefined;
    // A tick later, so a rail or full-screen change has resized the canvas.
    const timer = setTimeout(() => {
      const el = wrap.current;
      if (!el || nodes.length === 0) return;
      const bounds = getNodesBounds(nodes);
      const vp = getViewportForBounds(bounds, el.clientWidth, el.clientHeight, fitMinZoom ?? 0.05, 1.25, 0.08);
      cancel = tweenViewport(rf, vp, MOVE_MS);
    }, 40);
    return () => {
      clearTimeout(timer);
      cancel();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fitKey, followKey]);

  useEffect(() => {
    if (centerOn === null) return;
    const n = nodes.find((x) => x.id === String(centerOn));
    const el = wrap.current;
    if (!n || !el) return;
    const w = n.width ?? NODE_WIDTH;
    const h = n.height ?? 90;
    const cx = n.position.x + w / 2;
    const cy = n.position.y + h / 2;
    return tweenViewport(rf, { x: el.clientWidth / 2 - cx, y: el.clientHeight / 2 - cy, zoom: 1 }, 550);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [centerOn]);

  const flowNodes = useMemo(
    () => displayNodes.map((n) => (String(selectedId) === n.id ? { ...n, selected: true } : n)),
    [displayNodes, selectedId]
  );
  const onNodeClick: NodeMouseHandler<IssueFlowNode> = useCallback((_, n) => onSelect(Number(n.id)), [onSelect]);
  const onNodeDoubleClick: NodeMouseHandler<IssueFlowNode> = useCallback(
    (_, n) => {
      if (n.data.issue.children.length > 0) onFocus(Number(n.id));
    },
    [onFocus]
  );

  return (
    <div ref={wrap} className="absolute inset-0">
    <ReactFlow<IssueFlowNode>
      nodes={flowNodes}
      edges={edges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      colorMode={colorMode}
      fitView
      fitViewOptions={{ padding: 0.08, maxZoom: 1.25, minZoom: fitMinZoom }}
      style={{ background: "hsl(var(--background))" }}
      onNodeClick={onNodeClick}
      onNodeDoubleClick={onNodeDoubleClick}
      onPaneClick={() => onSelect(null)}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      minZoom={0.05}
      maxZoom={2}
      proOptions={{ hideAttribution: true }}
      defaultEdgeOptions={{ type: "curved" }}
    >
      <Background variant={BackgroundVariant.Dots} gap={22} size={1} />
      <Controls showInteractive={false} position="bottom-right" />
      <MiniMap
        pannable
        zoomable
        position="top-right"
        nodeColor={(n) => accentOf((n as IssueFlowNode).data.issue).color}
        nodeStrokeWidth={0}
        maskColor={colorMode === "dark" ? "rgba(0,0,0,0.6)" : "rgba(255,255,255,0.7)"}
        style={{ width: 180, height: 120 }}
      />
    </ReactFlow>
    </div>
  );
}

function Toggle({ active, onClick, label, icon, title }: { active: boolean; onClick: () => void; label: string; icon?: React.ReactNode; title?: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      title={title}
      className={cn(
        "inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[11.5px] font-medium transition-colors",
        active ? "border-foreground/40 bg-secondary text-foreground" : "border-border text-muted-foreground hover:bg-secondary/60"
      )}
    >
      {icon}
      <span className={cn("h-1.5 w-1.5 rounded-full", active ? "bg-foreground" : "bg-muted-foreground/40")} />
      {label}
    </button>
  );
}

export function IssueMapTab() {
  const live = useIssueGraph({ enabled: true });
  const { loading, error, load } = live;

  // Playback. `at` is the day the map shows, null for today. The graph for a
  // past day is derived in the browser from the dates the API already sends.
  const [at, setAt] = useState<number | null>(null);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(2);
  const [showTimeline, setShowTimeline] = useState(false);
  const atRaw = useRef<number | null>(null);
  const range = useMemo(() => (live.data ? timelineRange(live.data) : null), [live.data]);
  const series = useMemo(() => (live.data && range ? activitySeries(live.data, range) : null), [live.data, range]);
  const data = useMemo(() => (live.data && at !== null ? graphAsOf(live.data, at) : live.data), [live.data, at]);
  const index: IssueIndex = useMemo(() => {
    if (at === null) return live.index;
    const m = new Map<number, IssueGraphNode>();
    for (const n of data?.nodes ?? []) m.set(n.number, n);
    return m;
  }, [data, live.index, at]);

  useEffect(() => {
    if (!playing || !range) return;
    let last = performance.now();
    let raw = atRaw.current ?? range.start;
    let frame = 0;
    const step = (now: number) => {
      raw += ((now - last) * SPEEDS[speed].perSecond) / 1000;
      last = now;
      if (raw >= range.end) {
        atRaw.current = null;
        setAt(null);
        setPlaying(false);
        return;
      }
      atRaw.current = raw;
      // Re-derive the graph once per day, not once per frame.
      const day = dayFloor(raw);
      setAt((prev) => (prev === day ? prev : day));
      frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [playing, speed, range]);

  const scrub = useCallback(
    (ms: number) => {
      if (!range) return;
      setPlaying(false);
      if (ms >= range.end - 86_400_000) {
        atRaw.current = null;
        setAt(null);
        return;
      }
      const day = Math.max(range.start, dayFloor(ms));
      atRaw.current = day;
      setAt(day);
    },
    [range]
  );
  const togglePlay = useCallback(() => {
    if (!range) return;
    if (playing) {
      setPlaying(false);
      return;
    }
    if (at === null) {
      atRaw.current = range.start;
      setAt(range.start);
    }
    setPlaying(true);
  }, [range, playing, at]);
  const goLive = useCallback(() => {
    setPlaying(false);
    atRaw.current = null;
    setAt(null);
  }, []);
  const toggleTimeline = useCallback(() => {
    setShowTimeline((v) => {
      if (v) goLive();
      return !v;
    });
  }, [goLive]);
  const { theme } = useTheme();
  // Follow the class the provider puts on <html>, which is what every other
  // surface on the page follows; the provider's own value is the fallback.
  const colorMode: "light" | "dark" = useMemo(() => {
    if (typeof document !== "undefined" && document.documentElement.classList.contains("dark")) return "dark";
    if (typeof document !== "undefined" && document.documentElement.classList.contains("light")) return "light";
    return theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
  }, [theme]);

  const root = useRef<HTMLDivElement>(null);
  const [view, setView] = useState<View>("all");
  const [selected, setSelected] = useState<number | null>(null);
  // Left-to-right by default: most epics are one level deep and ten wide, which
  // top-down renders as a thin strip and left-right as a readable column.
  const [direction, setDirection] = useState<LayoutDirection>("LR");
  const [hideDone, setHideDone] = useState(false);
  const [showRelated, setShowRelated] = useState(false);
  const [showPRs, setShowPRs] = useState(true);
  const [railOpen, setRailOpen] = useState(true);
  const [fullscreen, setFullscreen] = useState(false);
  const [railQuery, setRailQuery] = useState("");
  const [jump, setJump] = useState("");
  const [centerOn, setCenterOn] = useState<number | null>(null);
  const centerTick = useRef(0);

  useEffect(() => {
    const onChange = () => setFullscreen(document.fullscreenElement === root.current);
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);
  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void root.current?.requestFullscreen?.();
  }, []);

  const selection = useMemo(() => {
    if (!data) return null;
    return selectGraph(data, index, { view, hideDone, showRelated, showPRs });
  }, [data, index, view, hideDone, showRelated, showPRs]);

  // The layout is today's, whatever day is shown: during playback the cards
  // appear where they will end up, so the tree never reshuffles under the eye.
  const layoutSelection = useMemo(() => {
    if (at === null || !live.data) return selection;
    return selectGraph(live.data, live.index, { view, hideDone: false, showRelated, showPRs });
  }, [at, live.data, live.index, selection, view, showRelated, showPRs]);
  const positions = useMemo(() => {
    if (!layoutSelection) return null;
    return layoutPositions(layoutSelection, at === null ? index : live.index, direction);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutSelection, direction]);
  const flow = useMemo(() => {
    if (!selection || !positions) return null;
    return buildFlow(selection, index, direction, positions);
  }, [selection, index, direction, positions]);

  const pickView = useCallback((v: View) => {
    setView(v);
    setSelected(null);
  }, []);

  const roots = useMemo(() => {
    if (!data) return [];
    const q = railQuery.trim().toLowerCase();
    return data.roots
      .map((n) => index.get(n))
      .filter((n): n is IssueGraphNode => !!n)
      .filter((n) => !q || n.title.toLowerCase().includes(q) || String(n.number).includes(q));
  }, [data, index, railQuery]);

  const selectedNode = selected !== null ? index.get(selected) ?? null : null;
  const viewRoot = typeof view === "number" ? index.get(view) ?? null : null;
  const crumbs = viewRoot ? parentChain(viewRoot.number, index) : [];

  const focus = useCallback((n: number) => {
    setView(n);
    setSelected(n);
  }, []);

  const isOnCanvas = useCallback(
    (num: number) => {
      if (!selection) return false;
      return selection.shown.has(num) || selection.ghosts.has(num) || selection.prs.some((p) => p.number === num);
    },
    [selection]
  );

  const reveal = useCallback(
    (num: number) => {
      const inView = isOnCanvas(num);
      if (!inView) {
        setView("all");
        setHideDone(false);
      }
      setSelected(num);
      centerTick.current += 1;
      setTimeout(() => setCenterOn(num), inView ? 0 : 350);
    },
    [isOnCanvas]
  );

  const onJump = useCallback(() => {
    const m = jump.match(/\d+/);
    if (!m || !data) return;
    const num = Number(m[0]);
    if (!index.has(num)) return;
    reveal(num);
    setJump("");
  }, [jump, data, index, reveal]);

  const fitKey = `${String(view)}|${direction}|${hideDone}|${showRelated}|${showPRs}|${railOpen}|${fullscreen}|${data?.fetched_at ?? ""}`;
  const shownCount = selection ? selection.shown.size : 0;
  const prCount = selection ? selection.prs.length : 0;

  if (error && !data) {
    return (
      <div className="m-6 rounded-2xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
        <div className="flex items-center gap-2 font-medium">
          <AlertCircle className="h-4 w-4" /> The issue map could not load
        </div>
        <p className="mt-1 text-xs">{error}</p>
        <Button size="sm" variant="outline" className="mt-3" onClick={() => void load(false)}>
          Try again
        </Button>
      </div>
    );
  }

  return (
    <div
      ref={root}
      className="flex flex-col bg-background"
      style={{ height: fullscreen ? "100vh" : "calc(100vh - 5rem)", minHeight: 560 }}
    >
      {/* Stats + actions */}
      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
        {data ? (
          <>
            <StatTile label="Open" value={data.stats.open} color={KIND_COLOR.open} />
            <StatTile label="Blocked" value={data.stats.blocked} color={STATUS.blocked.color} />
            <StatTile label="In review" value={data.stats.in_review} color={STATUS.in_review.color} />
            <StatTile label="PR merged, still open" value={data.stats.merged_open} color={STATUS.merged_open.color} />
            <StatTile label="Done" value={data.stats.done} color={KIND_COLOR.closed} />
            <StatTile label="Not planned" value={data.stats.dropped} color={KIND_COLOR.dropped} />
            <StatTile label="Open PRs" value={data.stats.open_pull_requests} color={KIND_COLOR.pr} />
            <StatTile label="Unparented" value={data.stats.orphans} color="#64748b" />
          </>
        ) : (
          <div className="text-sm text-muted-foreground">Reading the tracker…</div>
        )}
        <div className="ml-auto flex items-center gap-3">
          {data && (
            <div className="text-right text-[11px] leading-tight text-muted-foreground">
              <div>
                {data.repo} · {data.nodes.length} nodes · {data.edges.length} edges
              </div>
              <div>
                fetched {ago(data.fetched_at)}
                {data.cached ? " (cached)" : " from GitHub"}
                {data.truncated ? " · truncated" : ""}
              </div>
            </div>
          )}
          <Button size="sm" className="gap-2" onClick={() => void load(true)} disabled={loading}>
            <RefreshCw className={cn("h-3.5 w-3.5", loading && "animate-spin")} />
            {loading ? "Fetching…" : "Refresh from GitHub"}
          </Button>
          <Button size="sm" variant="outline" className="h-8 w-8 p-0" onClick={toggleFullscreen} title={fullscreen ? "Exit full screen" : "Full screen"}>
            {fullscreen ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
          </Button>
        </div>
      </div>
      {error && data && <div className="border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-xs text-destructive">{error}</div>}

      <div className="flex min-h-0 flex-1">
        {/* Rail */}
        {railOpen && (
          <aside className="flex w-[272px] shrink-0 flex-col border-r">
            <div className="border-b p-2.5">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={railQuery}
                  onChange={(e) => setRailQuery(e.target.value)}
                  placeholder="Filter epics and parents"
                  className="h-8 pl-8 text-[12px]"
                />
              </div>
            </div>
            <div className="flex-1 overflow-y-auto p-2">
              <button
                type="button"
                onClick={() => pickView("all")}
                className={cn(
                  "mb-1 flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-[12.5px] font-medium hover:bg-secondary/60",
                  view === "all" && "bg-secondary"
                )}
              >
                <GitFork className="h-3.5 w-3.5 text-muted-foreground" />
                Everything
                {data && <span className="ml-auto text-[11px] text-muted-foreground">{data.stats.open + data.stats.closed}</span>}
              </button>
              <button
                type="button"
                onClick={() => pickView("orphans")}
                className={cn(
                  "mb-2 flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-[12.5px] font-medium hover:bg-secondary/60",
                  view === "orphans" && "bg-secondary"
                )}
              >
                <span className="h-3.5 w-3.5 rounded-full border-2 border-dashed border-muted-foreground/60" />
                Unparented open issues
                {data && <span className="ml-auto text-[11px] text-muted-foreground">{data.stats.orphans}</span>}
              </button>
              <div className="mb-1 px-2 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground/70">
                Epics and parents
              </div>
              <div className="space-y-0.5">
                {roots.map((r) => {
                  const accent = accentOf(r);
                  const active = view === r.number;
                  return (
                    <button
                      key={r.number}
                      type="button"
                      onClick={() => pickView(r.number)}
                      className={cn(
                        "w-full rounded-md px-2.5 py-2 text-left transition-colors hover:bg-secondary/60",
                        active && "bg-secondary"
                      )}
                    >
                      <div className="flex items-center gap-2 text-[12px]">
                        <span className="font-semibold" style={{ color: accent.color }}>
                          #{r.number}
                        </span>
                        {r.is_epic && <Layers className="h-3 w-3 text-muted-foreground" />}
                        <span className="ml-auto text-[10.5px] text-muted-foreground">
                          {r.descendants.done}/{r.descendants.total}
                        </span>
                      </div>
                      <div className={cn("mt-0.5 line-clamp-2 text-[12px] leading-snug", r.state === "closed" && "text-muted-foreground")}>
                        {r.title}
                      </div>
                      <ProgressBar node={r} className="mt-1.5" />
                    </button>
                  );
                })}
                {data && roots.length === 0 && <div className="px-2 py-4 text-[12px] text-muted-foreground">No parent matches.</div>}
              </div>
            </div>
          </aside>
        )}

        {/* Canvas */}
        <section className="relative flex min-w-0 flex-1 flex-col">
          <div className="flex flex-wrap items-center gap-2 border-b px-2 py-1.5">
            <Button
              size="sm"
              variant="ghost"
              className="h-7 w-7 p-0"
              onClick={() => setRailOpen((v) => !v)}
              title={railOpen ? "Hide the list" : "Show the list"}
            >
              {railOpen ? <PanelLeftClose className="h-4 w-4" /> : <PanelLeftOpen className="h-4 w-4" />}
            </Button>
            <div className="flex min-w-0 items-center gap-1 text-[12px]">
              {view === "all" && <span className="font-medium">Every issue and pull request</span>}
              {view === "orphans" && <span className="font-medium">Open issues nothing claims</span>}
              {viewRoot && (
                <>
                  {crumbs.map((c) => (
                    <span key={c.number} className="flex items-center gap-1">
                      <button
                        type="button"
                        className="rounded px-1 text-muted-foreground hover:bg-secondary"
                        onClick={() => focus(c.number)}
                        title={c.title}
                      >
                        #{c.number}
                      </button>
                      <ChevronRight className="h-3 w-3 text-muted-foreground" />
                    </span>
                  ))}
                  <span className="font-semibold" style={{ color: accentOf(viewRoot).color }}>
                    #{viewRoot.number}
                  </span>
                  <span className="line-clamp-1 font-medium">{viewRoot.title}</span>
                </>
              )}
              {selection && (
                <span className="ml-2 whitespace-nowrap text-[11px] text-muted-foreground">
                  {shownCount} issues{showPRs ? `, ${prCount} PRs` : ""}
                </span>
              )}
              {at !== null && (
                <span className="ml-1 whitespace-nowrap rounded-md bg-amber-500/15 px-1.5 py-[2px] text-[10.5px] font-semibold text-amber-600 dark:text-amber-400">
                  as of {formatDay(at)}
                </span>
              )}
            </div>
            <div className="ml-auto flex flex-wrap items-center gap-1.5">
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  onJump();
                }}
                className="relative"
              >
                <Input value={jump} onChange={(e) => setJump(e.target.value)} placeholder="Jump to #" className="h-7 w-[104px] text-[12px]" />
              </form>
              <Toggle
                active={showTimeline}
                onClick={toggleTimeline}
                label="Timeline"
                icon={<History className="h-3 w-3" />}
                title="Replay how the tracker grew, day by day"
              />
              <Toggle active={hideDone} onClick={() => setHideDone((v) => !v)} label="Hide finished" />
              <Toggle active={showPRs} onClick={() => setShowPRs((v) => !v)} label="Pull requests" icon={<GitPullRequestArrow className="h-3 w-3" />} />
              <Toggle active={showRelated} onClick={() => setShowRelated((v) => !v)} label="Mentions" />
              <Button
                  size="sm"
                  variant="outline"
                  className="h-7 gap-1.5 px-2 text-[11.5px]"
                  onClick={() => setDirection((d) => (d === "TB" ? "LR" : "TB"))}
                  title="Layout direction"
                >
                  {direction === "TB" ? <ArrowDownToLine className="h-3.5 w-3.5" /> : <ArrowRightToLine className="h-3.5 w-3.5" />}
                  {direction === "TB" ? "Top-down" : "Left-right"}
                </Button>
            </div>
          </div>

          <div className="relative min-h-0 flex-1">
            {flow && (
              <ReactFlowProvider>
                <Canvas
                  nodes={flow.nodes}
                  edges={flow.edges}
                  selectedId={selected}
                  onSelect={setSelected}
                  onFocus={focus}
                  colorMode={colorMode}
                  fitKey={fitKey}
                  followKey={playing ? at : null}
                  centerOn={centerOn}
                  fitMinZoom={typeof view === "number" ? 0.7 : undefined}
                />
              </ReactFlowProvider>
            )}
            {!selection && (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                {loading ? "Laying out the tracker…" : "Nothing to show yet."}
              </div>
            )}
            {selection && shownCount === 0 && (
              <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">
                {at !== null
                  ? `Nothing here yet on ${formatDay(at)}.`
                  : `Nothing left to show here${hideDone ? " with finished work hidden" : ""}.`}
              </div>
            )}
          </div>

          {showTimeline && range && series && (
            <Timeline
              range={range}
              series={series}
              at={at}
              playing={playing}
              speed={speed}
              onScrub={scrub}
              onPlay={togglePlay}
              onSpeed={setSpeed}
              onLive={goLive}
            />
          )}
          <div className="border-t px-3 py-1">
            <Legend />
          </div>
        </section>

        {selectedNode && data && (
          <DetailPanel
            node={selectedNode}
            index={index}
            edges={data.edges}
            onSelect={reveal}
            onFocus={focus}
            onClose={() => setSelected(null)}
          />
        )}
      </div>
    </div>
  );
}
