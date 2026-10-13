import dagre from "@dagrejs/dagre";

export type LayoutDirection = "TB" | "LR";

export type LayoutInput = {
  id: string;
  width: number;
  height: number;
};

export type LayoutEdge = { source: string; target: string };

export type Positioned = Map<string, { x: number; y: number }>;

// Hierarchical layout. Only the primary edges shape it: dependency and
// follow-up edges are drawn over the result afterwards, so a "blocked by" arrow
// never drags an issue out of its epic's column.
//
// Dagre lays every disconnected tree side by side in one row, which for the
// whole tracker (forty epics and fifty loose issues) is a strip thirty screens
// wide and one tall. So each connected component is laid out on its own and the
// pieces are packed into rows, largest first, like a mosaic.
export function layoutGraph(nodes: LayoutInput[], edges: LayoutEdge[], direction: LayoutDirection): Positioned {
  const ids = new Set(nodes.map((n) => n.id));
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const kept = edges.filter((e) => ids.has(e.source) && ids.has(e.target));

  // Connected components by union-find.
  const parent = new Map<string, string>();
  const find = (x: string): string => {
    let r = x;
    while (parent.get(r) !== r) r = parent.get(r)!;
    while (parent.get(x) !== r) {
      const next = parent.get(x)!;
      parent.set(x, r);
      x = next;
    }
    return r;
  };
  for (const n of nodes) parent.set(n.id, n.id);
  for (const e of kept) {
    const a = find(e.source);
    const b = find(e.target);
    if (a !== b) parent.set(a, b);
  }
  const groups = new Map<string, LayoutInput[]>();
  for (const n of nodes) {
    const r = find(n.id);
    if (!groups.has(r)) groups.set(r, []);
    groups.get(r)!.push(n);
  }
  const edgesByRoot = new Map<string, LayoutEdge[]>();
  for (const e of kept) {
    const r = find(e.source);
    if (!edgesByRoot.has(r)) edgesByRoot.set(r, []);
    edgesByRoot.get(r)!.push(e);
  }

  type Piece = { positions: Positioned; width: number; height: number; size: number };
  const pieces: Piece[] = [];
  for (const [root, members] of groups) {
    pieces.push(layoutComponent(members, edgesByRoot.get(root) ?? [], direction, byId));
  }
  if (pieces.length === 1) return pieces[0].positions;

  // Pack: big trees first, singletons last, into rows whose width makes the
  // whole mosaic roughly screen-shaped (16:10), never narrower than the widest
  // tree.
  pieces.sort((a, b) => b.size - a.size || b.width - a.width);
  const gap = 56;
  const area = pieces.reduce((sum, p) => sum + (p.width + gap) * (p.height + gap), 0);
  const maxRow = Math.max(Math.sqrt(area * 1.6), ...pieces.map((p) => p.width));
  const out: Positioned = new Map();
  let x = 0;
  let y = 0;
  let rowHeight = 0;
  for (const p of pieces) {
    if (x > 0 && x + p.width > maxRow) {
      x = 0;
      y += rowHeight + gap;
      rowHeight = 0;
    }
    for (const [id, pos] of p.positions) out.set(id, { x: pos.x + x, y: pos.y + y });
    x += p.width + gap;
    rowHeight = Math.max(rowHeight, p.height);
  }
  return out;
}

function layoutComponent(
  members: LayoutInput[],
  edges: LayoutEdge[],
  direction: LayoutDirection,
  byId: Map<string, LayoutInput>
): { positions: Positioned; width: number; height: number; size: number } {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({
    rankdir: direction,
    // Generous gaps: the nodes carry two lines of text and a status pill, and a
    // column that touches is one the eye reads as a single block.
    nodesep: direction === "TB" ? 44 : 36,
    ranksep: direction === "TB" ? 100 : 160,
    marginx: 0,
    marginy: 0,
    ranker: "tight-tree",
  });
  for (const n of members) g.setNode(n.id, { width: n.width, height: n.height });
  for (const e of edges) g.setEdge(e.source, e.target);
  dagre.layout(g);
  const positions: Positioned = new Map();
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const n of members) {
    const pos = g.node(n.id);
    const dims = byId.get(n.id)!;
    // dagre returns centres; React Flow positions the top-left corner.
    const x = pos.x - dims.width / 2;
    const y = pos.y - dims.height / 2;
    positions.set(n.id, { x, y });
    minX = Math.min(minX, x);
    minY = Math.min(minY, y);
    maxX = Math.max(maxX, x + dims.width);
    maxY = Math.max(maxY, y + dims.height);
  }
  // Normalise to the origin so pieces can be packed.
  for (const [id, pos] of positions) positions.set(id, { x: pos.x - minX, y: pos.y - minY });
  return { positions, width: maxX - minX, height: maxY - minY, size: members.length };
}
