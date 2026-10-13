import { memo } from "react";
import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from "@xyflow/react";

// Every edge on the tree is a cubic curve between its two ends. A curve
// leaves a node at a right angle and arrives at one, so a column of ten
// children reads as a fan rather than a comb of elbows, and a blocker drawn
// across the mosaic bends around the nodes instead of cutting through them.
function CurvedEdgeComponent({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style,
  markerEnd,
  label,
  labelStyle,
  interactionWidth,
}: EdgeProps) {
  const [path, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    curvature: 0.32,
  });
  const color = (labelStyle as { fill?: string } | undefined)?.fill;
  return (
    <>
      <BaseEdge id={id} path={path} style={style} markerEnd={markerEnd} interactionWidth={interactionWidth ?? 14} />
      {label && (
        <EdgeLabelRenderer>
          <div
            className="pointer-events-none absolute rounded bg-card/95 px-[5px] py-[1px] text-[10px] font-semibold shadow-sm"
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, color }}
          >
            {label}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

export const CurvedEdge = memo(CurvedEdgeComponent);
