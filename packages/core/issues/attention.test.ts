// @vitest-environment node
import { describe, expect, it } from "vitest";
import { attentionCountsByIssue } from "./attention";

// PRD A test data: P1 > S3 (in review), P3 > C1 > G2, P5 > R1/R3, P2 alone.
describe("attentionCountsByIssue", () => {
  const counts = attentionCountsByIssue({
    in_review: [
      { issue_id: "s3", ancestor_ids: ["p1"] },
      { issue_id: "r3", ancestor_ids: ["p5"] },
      { issue_id: "g3", ancestor_ids: ["c1", "p3"] },
    ],
    awaiting_reply: [
      { issue_id: "r1", ancestor_ids: ["p5"] },
      { issue_id: "g2", ancestor_ids: ["c1", "p3"] },
      { issue_id: "p2", ancestor_ids: [] },
    ],
  });
  const of = (id: string) => counts.get(id) ?? { awaitingReview: 0, awaitingReply: 0 };

  it("A2, A20: an in-review issue counts for its ancestors", () => {
    expect(of("p1")).toEqual({ awaitingReview: 1, awaitingReply: 0 });
    expect(of("p5")).toEqual({ awaitingReview: 1, awaitingReply: 1 });
  });

  it("A37: every level up, never the in-review issue itself", () => {
    expect(of("p3").awaitingReview).toBe(1);
    expect(of("c1").awaitingReview).toBe(1);
    expect(of("g3").awaitingReview).toBe(0);
    expect(of("s3").awaitingReview).toBe(0);
  });

  it("A21: a reply counts for the issue itself and every ancestor", () => {
    expect(of("p3").awaitingReply).toBe(1);
    expect(of("c1").awaitingReply).toBe(1);
    expect(of("g2").awaitingReply).toBe(1);
    expect(of("p2")).toEqual({ awaitingReview: 0, awaitingReply: 1 });
  });

  it("a repeated ancestor counts once", () => {
    const c = attentionCountsByIssue({
      in_review: [{ issue_id: "x", ancestor_ids: ["p", "p"] }],
      awaiting_reply: [{ issue_id: "p", ancestor_ids: ["p"] }],
    });
    expect(c.get("p")).toEqual({ awaitingReview: 1, awaitingReply: 1 });
  });
});
