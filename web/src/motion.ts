import { useEffect, useRef, useState } from "react";
import type { ReactFlowInstance, Viewport } from "@xyflow/react";
import type { IssueFlowNode } from "./IssueNode";

// Motion for the tree. Two things move: the nodes, which glide from where they
// were to where the new layout puts them, and the camera, which eases to the
// new framing. Both are driven by requestAnimationFrame here rather than by
// CSS or d3-transition: a CSS transform transition would move the cards but
// leave the edges where they were until it ended, and the d3 route depends on
// which copy of d3-selection the bundler resolved (see vite.config.ts).

export const easeInOut = (t: number) => (t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2);

export const reducedMotion = () =>
  typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

// Above this many nodes a relayout is applied in one step: moving six hundred
// cards sixty times a second makes the whole page stutter, which is worse than
// a cut.
const TWEEN_LIMIT = 420;
// How long an outgoing card stays while it fades.
const EXIT_MS = 260;

export function tweenViewport(rf: ReactFlowInstance, to: Viewport, ms: number): () => void {
  if (ms <= 0 || reducedMotion()) {
    void rf.setViewport(to);
    return () => undefined;
  }
  const from = rf.getViewport();
  const start = performance.now();
  let frame = 0;
  const step = (now: number) => {
    const t = Math.min(1, (now - start) / ms);
    const k = easeInOut(t);
    // Zoom moves geometrically so the perceived speed is even across the
    // range; the pan interpolates linearly.
    const zoom = from.zoom * Math.pow(to.zoom / from.zoom, k);
    void rf.setViewport({ x: from.x + (to.x - from.x) * k, y: from.y + (to.y - from.y) * k, zoom });
    if (t < 1) frame = requestAnimationFrame(step);
  };
  frame = requestAnimationFrame(step);
  return () => cancelAnimationFrame(frame);
}

// Returns the nodes to render: the target layout, with cards that were already
// on the canvas sliding in from their previous position and cards that left
// lingering for a moment with `exiting` set so they can fade.
export function useTweenedNodes(target: IssueFlowNode[], ms: number): IssueFlowNode[] {
  const [display, setDisplay] = useState<IssueFlowNode[]>(target);
  // What is on the canvas right now, mid-flight included, so a layout that
  // arrives during a move starts from where the cards are, not where they
  // were going.
  const onCanvas = useRef<IssueFlowNode[]>(target);
  const show = (nodes: IssueFlowNode[]) => {
    onCanvas.current = nodes;
    setDisplay(nodes);
  };
  const first = useRef(true);

  useEffect(() => {
    const prev = new Map(onCanvas.current.map((n) => [n.id, n]));
    const next = new Map(target.map((n) => [n.id, n]));

    if (first.current) {
      first.current = false;
      show(target);
      return;
    }

    const moving = target.filter((n) => {
      const p = prev.get(n.id);
      return p && (p.position.x !== n.position.x || p.position.y !== n.position.y);
    });
    const leaving = [...prev.values()].filter((n) => !next.has(n.id) && !n.data.exiting);

    if ((moving.length === 0 && leaving.length === 0) || reducedMotion() || target.length > TWEEN_LIMIT) {
      show(target);
      return;
    }

    const exits: IssueFlowNode[] = leaving.map((n) => ({ ...n, data: { ...n.data, exiting: true }, selectable: false }));
    const from = new Map(moving.map((n) => [n.id, prev.get(n.id)!.position]));
    const start = performance.now();
    const total = Math.max(ms, EXIT_MS);
    let frame = 0;
    const step = (now: number) => {
      const elapsed = now - start;
      const t = Math.min(1, elapsed / ms);
      const k = easeInOut(t);
      const frameNodes = target.map((n) => {
        const f = from.get(n.id);
        if (!f || t >= 1) return n;
        return { ...n, position: { x: f.x + (n.position.x - f.x) * k, y: f.y + (n.position.y - f.y) * k } };
      });
      const withExits = elapsed < EXIT_MS ? [...frameNodes, ...exits] : frameNodes;
      show(withExits);
      if (elapsed < total) frame = requestAnimationFrame(step);
      else show(target);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [target, ms]);

  return display;
}
