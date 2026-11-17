import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Comment, InstanceBatch } from "../api";
import { commentInstance } from "../model";
import { keys } from "../queries";
import { InstanceTag } from "./Comments";
import { WorkspaceContext, type WorkspaceData } from "./context";

// BUNN-31 D3 (一鸣 11-17): a comment an instance wrote shows "实例 i/k · <分工>"
// next to its author, found through source_task_id in the issue's instances.
const batch: InstanceBatch = {
  id: "b1",
  agent: { id: "tb", name: "TB" },
  total: 3,
  instances: [
    { index: 1, split: "a", status: "completed", queue: null, task_id: "t1b", task_ids: ["t1a", "t1b"] },
    { index: 2, split: "b", status: "running", queue: null, task_id: "t2", task_ids: ["t2"] },
    { index: 3, split: "把导出报表页面的列宽和字体都调整成和设计稿一致，再补上单测和截图", status: "queued", queue: null, task_id: "t3", task_ids: ["t3"] },
  ],
};

describe("commentInstance", () => {
  it("finds the instance from any attempt, with the batch's k", () => {
    expect(commentInstance([batch], "t1a")).toEqual({ index: 1, total: 3, split: "a" });
    expect(commentInstance([batch], "t1b")).toEqual({ index: 1, total: 3, split: "a" });
    expect(commentInstance([batch], "t2")).toEqual({ index: 2, total: 3, split: "b" });
  });

  it("is null for comments no instance wrote", () => {
    expect(commentInstance([batch], "someone-else")).toBeNull();
    expect(commentInstance([batch], null)).toBeNull();
    expect(commentInstance([batch], undefined)).toBeNull();
    expect(commentInstance([], "t2")).toBeNull();
    // Older servers without task_ids: the latest attempt still matches.
    expect(commentInstance([{ ...batch, instances: [{ index: 1, split: "a", status: "running", queue: null, task_id: "t9" }] }], "t9"))
      .toEqual({ index: 1, total: 3, split: "a" });
  });
});

describe("InstanceTag", () => {
  const render = (comment: Partial<Comment>) => {
    const qc = new QueryClient();
    qc.setQueryData(keys.instances("ws", "issue-1"), [batch]);
    return renderToStaticMarkup(
      <QueryClientProvider client={qc}>
        <WorkspaceContext.Provider value={{ slug: "ws" } as WorkspaceData}>
          <InstanceTag comment={{ id: "c", issue_id: "issue-1", ...comment } as Comment} />
        </WorkspaceContext.Provider>
      </QueryClientProvider>,
    );
  };

  it("labels an instance's comment, \"实例 i/k\" apart from the split that may truncate", () => {
    expect(render({ source_task_id: "t2" })).toBe(
      '<span class="instance-tag" title="实例 2/3 · b" data-testid="instance-tag"><span class="instance-n">实例 2/3</span><span class="instance-split"> · b</span></span>',
    );
    expect(render({ source_task_id: "t3" })).toContain('<span class="instance-n">实例 3/3</span>');
  });

  it("labels nothing else", () => {
    expect(render({ source_task_id: "other" })).toBe("");
    expect(render({})).toBe("");
  });
});
