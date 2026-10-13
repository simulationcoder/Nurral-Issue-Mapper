import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { graphAsOf } from "./timeline";
import type { IssueGraphResponse } from "./types";

// internal/graph/testdata/parity.json is written by the Go tests. The browser's
// timeline derives statuses a second time; replayed at "now" it must agree.
const golden = JSON.parse(
  readFileSync(new URL("../../internal/graph/testdata/parity.json", import.meta.url), "utf8"),
) as IssueGraphResponse;

describe("timeline parity with the Go status rules", () => {
  it("replays the final day to the statuses Go derived", () => {
    const replay = graphAsOf(golden, Date.parse(golden.fetched_at));
    const got = new Map(replay.nodes.map((n) => [n.number, n.status]));
    for (const n of golden.nodes) {
      expect(got.get(n.number), `#${n.number} ${n.title}`).toBe(n.status);
    }
  });

  it("does not call a merged PR that only mentions an issue 'merged but open'", () => {
    const replay = graphAsOf(golden, Date.parse(golden.fetched_at));
    expect(replay.nodes.find((n) => n.number === 5)?.status).toBe("open");
    expect(replay.nodes.find((n) => n.number === 4)?.status).toBe("merged_open");
  });
});
