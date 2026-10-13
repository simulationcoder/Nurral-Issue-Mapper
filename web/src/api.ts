import type { IssueGraphResponse } from "./types";

/** Reads the graph from the server; `refresh` makes it re-read GitHub. */
export async function issueGraph(refresh = false): Promise<IssueGraphResponse> {
  const res = await fetch(`/api/graph${refresh ? "?refresh=1" : ""}`);
  if (!res.ok) {
    let msg = `${res.status}`;
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg;
    } catch {
      /* not JSON */
    }
    throw new Error(msg);
  }
  return (await res.json()) as IssueGraphResponse;
}
