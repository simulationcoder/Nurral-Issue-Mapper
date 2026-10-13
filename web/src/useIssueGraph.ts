import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { issueGraph } from "./api";
import type { IssueGraphNode, IssueGraphResponse } from "./types";

export type IssueIndex = Map<number, IssueGraphNode>;

export function useIssueGraph({ enabled }: { enabled: boolean }) {
  const [data, setData] = useState<IssueGraphResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const loadedOnce = useRef(false);

  const load = useCallback(async (refresh = false) => {
    setLoading(true);
    setError("");
    try {
      const payload = await issueGraph(refresh);
      setData(payload);
      loadedOnce.current = true;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load the issue graph");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (enabled && !loadedOnce.current && !loading) void load(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled]);

  const index: IssueIndex = useMemo(() => {
    const m = new Map<number, IssueGraphNode>();
    for (const n of data?.nodes ?? []) m.set(n.number, n);
    return m;
  }, [data]);

  return { data, index, loading, error, load };
}
