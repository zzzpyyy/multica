import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { PlanCard } from "@multica/core/types/super-agent";
import zhChat from "../../locales/zh-Hans/chat.json";
import zhIssues from "../../locales/zh-Hans/issues.json";
import { PlanCardView } from "./plan-cards";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));

const base = {
  id: "card-1",
  chat_session_id: "sess-1",
  chat_message_id: "msg-1",
  error: null,
  created_at: "2026-11-10T00:00:00Z",
};
const createCard = (over: Partial<Extract<PlanCard, { kind: "create_issues" }>> = {}): PlanCard => ({
  ...base,
  kind: "create_issues",
  status: "pending",
  items: [{
    title: "看板支持暗色模式",
    assignee: { type: "agent", id: "a-1", name: "尤雨溪" },
    what: "看板跟随系统深色",
    done: "系统切到深色时看板变深色",
    status: "backlog",
    will_run: false,
  }],
  created_issues: [],
  ...over,
});

function renderCard(card: PlanCard, decidePlanCard = vi.fn()) {
  setApiInstance({ decidePlanCard } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <I18nProvider locale="zh-Hans" resources={{ "zh-Hans": { chat: zhChat, issues: zhIssues } }}>
      <QueryClientProvider client={qc}>
        <PlanCardView card={card} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return decidePlanCard;
}

describe("PlanCardView (F6)", () => {
  it("shows every field of a pending create card (B2, rule 5)", () => {
    renderCard(createCard());
    for (const text of ["看板支持暗色模式", "尤雨溪", "看板跟随系统深色", "系统切到深色时看板变深色", "不会运行", "待确认"]) {
      expect(screen.getByText(text, { exact: false })).toBeInTheDocument();
    }
    expect(screen.getByRole("button", { name: "确认创建" })).toBeEnabled();
  });

  it("marks a card that will start a run (B4)", () => {
    const item = (createCard() as Extract<PlanCard, { kind: "create_issues" }>).items[0]!;
    renderCard(createCard({ items: [{ ...item, status: "todo", will_run: true }] }));
    expect(screen.getByText("确认后立即开始运行")).toBeInTheDocument();
  });

  it("confirms through the API", async () => {
    const decide = renderCard(createCard(), vi.fn().mockResolvedValue(createCard({ status: "created" })));
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));
    await waitFor(() => expect(decide).toHaveBeenCalledWith("card-1", "confirm"));
  });

  it("a created card links the new issue and has no buttons (B5)", () => {
    renderCard(createCard({ status: "created", created_issues: [{ id: "i-12", identifier: "BUNN-12" }] }));
    expect(screen.getByText("BUNN-12").closest("a")).toHaveAttribute("href", "/acme/issues/i-12");
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("an expired card cannot be confirmed (B7)", () => {
    renderCard(createCard({ status: "expired" }));
    expect(screen.getByText("已失效")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("renders an add-instances card with splits and priority (D13, D16)", () => {
    renderCard({
      ...base,
      kind: "add_instances",
      status: "pending",
      issue: { id: "i-9", identifier: "BUNN-9", title: "X" },
      agent: { id: "a-2", name: "TB" },
      splits: ["d", "e"],
      priority: true,
      batch_id: null,
    });
    expect(screen.getByText("加开 2 个 TB：", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("优先")).toBeInTheDocument();
    expect(screen.getByText("d")).toBeInTheDocument();
    expect(screen.getByText("e")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "确认加开" })).toBeEnabled();
  });
});
