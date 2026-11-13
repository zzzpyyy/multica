import { describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import { nestedSummary, progressSegments, BUILT_IN_STATUS_ORDER } from "@multica/core/issues";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { NestedCardControls, NestedCardSummary } from "./card-summary";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog([]),
}));

// PRD v3.4 A rule 7, the expected sentences of A2, A3, A6, A7, A9, A20–A23,
// A34 (before rules 4–6) and A37 verbatim, from the PRD test data.
let seq = 0;
const child = (title: string, status: string, stage: number | null = null): Issue =>
  ({ id: `c${++seq}`, number: seq, identifier: `T-${seq}`, title, status, stage }) as Issue;

function line(
  children: Issue[],
  parent: string,
  attention: { awaitingReview?: number; awaitingReply?: number } = {},
) {
  const segments = progressSegments(children, {
    parentIdentifier: parent,
    statusOrder: BUILT_IN_STATUS_ORDER,
    ...attention,
  });
  const view = renderWithI18n(
    <NestedCardSummary segments={segments} />,
    { locale: "zh-Hans" },
  );
  const text = screen.queryByTestId("nested-progress-line")?.textContent ?? null;
  view.unmount();
  return text;
}

const P1 = "BUNN-101";
const p1 = () => [
  child("S1", "done", 1),
  child("S2", "cancelled", 1),
  child("阶段2：接口联调", "in_review", 2),
  child("S4", "blocked", 2),
  child(`${P1} 阶段 3: 把导出报表页面的列宽和字体都调整成和设计稿一致`, "backlog", 3),
];
const p5 = () => [child("R1", "in_progress"), child("R2", "in_progress"), child("R3", "in_review")];

describe("one-line progress, PRD v3.4 wording", () => {
  it("A2", () => {
    expect(line(p1(), P1, { awaitingReview: 1 })).toBe("阶段 2/3 · 审核中：接口联调 · 1 个阻塞 · 1 项待你验收");
  });

  it("A3: P2 has no line, P4 has one", () => {
    expect(line([], "BUNN-102")).toBeNull();
    expect(line([child("Q1", "done"), child("整理文案", "todo")], "BUNN-104")).toBe("待办：整理文案");
  });

  it("A6, A7", () => {
    const kids = p1();
    kids[3] = { ...kids[3]!, status: "done" };
    expect(line(kids, P1, { awaitingReview: 1 })).toBe("阶段 2/3 · 审核中：接口联调 · 1 项待你验收");
    kids[2] = { ...kids[2]!, status: "done" };
    expect(line(kids, P1)).toBe("阶段 3/3 · 待规划：把导出报表页面的列宽和字体都调整成和设计…");
  });

  it("A9: S5 detached", () => {
    const kids = p1().slice(0, 4).map((c) => ({ ...c, status: c.status === "cancelled" ? "cancelled" : "done" }));
    expect(line(kids, P1)).toBe("阶段已全部完成");
  });

  it("A20–A23: P5", () => {
    expect(line(p5(), "BUNN-105", { awaitingReview: 1 })).toBe("2 个进行中、1 个审核中 · 1 项待你验收");
    expect(line(p5(), "BUNN-105", { awaitingReview: 1, awaitingReply: 1 })).toBe(
      "2 个进行中、1 个审核中 · 1 项待你验收 · 1 项待你回复",
    );
  });

  it("A21: P3 counts its grandchild, P2 shows a line without children", () => {
    expect(line([child("C1", "todo")], "BUNN-103", { awaitingReply: 1 })).toBe("待办：C1 · 1 项待你回复");
    expect(line([], "BUNN-102", { awaitingReply: 1 })).toBe("1 项待你回复");
  });

  it("A34 before rules 4–6", () => {
    expect(line([child("V1", "in_review")], "BUNN-106", { awaitingReview: 1 })).toBe("审核中：V1 · 1 项待你验收");
  });

  it("A37: G3 in review below C1, then done", () => {
    expect(line([child("C1", "todo")], "BUNN-103", { awaitingReview: 1 })).toBe("待办：C1 · 1 项待你验收");
    expect(line([child("C1", "todo")], "BUNN-103")).toBe("待办：C1");
  });

  it("PRD examples: BUNN-20 before rules 4–6, and the reply example", () => {
    const bunn20 = [
      child("BUNN-20 阶段 1：Tiki workspace 接入需求与方案设计", "in_review", 1),
      ...Array.from({ length: 13 }, (_, i) => child(`阶段 ${i + 2}：x`, "in_review", i + 2)),
      child("阶段 15：y", "blocked", 15),
      child("阶段 16：z", "blocked", 16),
    ];
    expect(line(bunn20, "BUNN-20", { awaitingReview: 14 })).toBe(
      "阶段 1/16 · 审核中：Tiki workspace 接入需求与… · 2 个阻塞 · 14 项待你验收",
    );
    expect(
      line(
        [child("a", "done", 1), child("b", "done", 2), child("阶段3：服务端开发（按阶段2契约）", "in_progress", 3), child("d", "todo", 4)],
        "BUNN-26",
        { awaitingReply: 1 },
      ),
    ).toBe("阶段 3/4 · 进行中：服务端开发（按阶段2契约） · 1 项待你回复");
  });

  it("the calls to action are emphasized, the rest is not", () => {
    const segments = progressSegments(p5(), {
      parentIdentifier: "BUNN-105",
      statusOrder: BUILT_IN_STATUS_ORDER,
      awaitingReview: 1,
      awaitingReply: 1,
    });
    renderWithI18n(
      <NestedCardSummary segments={segments} />,
      { locale: "zh-Hans" },
    );
    expect(screen.getByText("1 项待你验收").className).toContain("text-warning");
    expect(screen.getByText("1 项待你回复").className).toContain("text-warning");
    expect(screen.getByText("2 个进行中、1 个审核中").className).not.toContain("text-warning");
  });

  // The layout fix of 13 Nov: P1's line had wrapped mid-word ("接 / 口联调",
  // "1 项 / 待你验收") on the row it shared with the cancelled count and
  // "展开". Each segment is now an unbreakable-unless-it-must inline-block on
  // lines of its own; a real browser check of the widths is in BUNN-31. Where
  // the cancelled count and "展开" went is tested in board-card-nested.test.tsx.
  it("puts the line on rows of its own, one inline-block per segment, nothing truncated", () => {
    const children = p1();
    const segments = progressSegments(children, {
      parentIdentifier: P1,
      statusOrder: BUILT_IN_STATUS_ORDER,
      awaitingReview: 1,
    });
    renderWithI18n(
      <NestedCardSummary segments={segments} />,
      { locale: "zh-Hans" },
    );
    const progress = screen.getByTestId("nested-progress-line");
    const parts = within(progress).getAllByTestId("nested-progress-segment");
    expect(parts.map((part) => part.textContent)).toEqual([
      "阶段 2/3 ·",
      "审核中：接口联调 ·",
      "1 个阻塞 ·",
      "1 项待你验收",
    ]);
    for (const part of parts) expect(part.className).toContain("inline-block");
    expect(within(parts[3]!).getByText("1 项待你验收").className).toContain("text-warning");
    expect(progress.className).not.toMatch(/truncate|line-clamp|ellipsis|nowrap/);
    expect(progress.className).toContain("block");
    expect(screen.queryByText("含 1 个已取消")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("a card without children has no expand toggle or cancelled count", () => {
    const view = renderWithI18n(
      <NestedCardControls summary={nestedSummary([])} expanded={false} onToggle={() => {}} />,
      { locale: "zh-Hans" },
    );
    expect(view.container.innerHTML).toBe("");
  });
});
