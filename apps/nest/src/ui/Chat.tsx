// The right column (PRD v4 section 4): the one super-agent conversation.
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ApiError, ensureSuperAgent, messagesPage, sendMessage, type ChatMessage, type PlanCard } from "../api";
import { draftKey } from "../composer";
import { keys, q } from "../queries";
import { mergeMessage } from "../realtime";
import { Composer, type SendResult } from "./Composer";
import { useWorkspace } from "./context";
import { Markdown } from "./Markdown";
import { PlanCardView } from "./PlanCard";

/** Where the chat column should scroll, from a `/<slug>/chat?...` address. */
export interface ChatFocus {
  sessionId: string | null;
  messageId: string | null;
}

export function ChatPanel({ focus }: { focus: ChatFocus | null }) {
  const { slug } = useWorkspace();
  const boot = useQuery(q.superAgent(slug));
  if (boot.isPending) return <aside className="chat"><p className="muted">加载对话…</p></aside>;
  if (boot.isError) {
    const e = boot.error;
    if (e instanceof ApiError && e.code === "runtime_required") return <Onboarding />;
    return (
      <aside className="chat">
        <p className="error">对话没加载出来</p>
        <button type="button" onClick={() => void boot.refetch()}>重试</button>
      </aside>
    );
  }
  return <Conversation sessionId={boot.data.sessionId} focus={focus} />;
}

function Onboarding() {
  const { slug } = useWorkspace();
  const qc = useQueryClient();
  const { data: runtimes = [], isPending } = useQuery(q.runtimes(slug));
  const usable = runtimes.filter((r) => r.provider === "claude" && r.status === "online");
  const [choice, setChoice] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const selected = usable.some((r) => r.id === choice) ? choice : usable[0]?.id ?? "";
  const start = async () => {
    setBusy(true);
    setError(null);
    try {
      qc.setQueryData(keys.superAgent(slug), await ensureSuperAgent(slug, selected));
    } catch (e) {
      setError(e instanceof ApiError ? `没开通成功：${e.message}` : "没开通成功，再试一次");
    } finally {
      setBusy(false);
    }
  };
  return (
    <aside className="chat onboarding" aria-label="对话">
      <h2>这个工作区还没开通 super-agent</h2>
      {isPending ? (
        <p className="muted">正在找可用的运行环境…</p>
      ) : usable.length === 0 ? (
        <p className="muted">没有在线的 Claude 运行环境，暂时不能开通。先在旧页面或用 CLI 接入一个 Claude 运行环境。</p>
      ) : (
        <label>
          运行环境
          <select value={selected} onChange={(e) => setChoice(e.target.value)}>
            {usable.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
          </select>
        </label>
      )}
      <button type="button" className="primary" disabled={busy || !selected} onClick={() => void start()}>
        开通
      </button>
      {error && <p className="error">{error}</p>}
    </aside>
  );
}

interface LocalMessage {
  key: string;
  content: string;
  state: "sending" | "failed";
  /** When the first attempt left, to recognize it after a lost response. */
  since: string;
}

const AT_BOTTOM = 40;
const MAX_PAGES = 40;
const byTime = (a: ChatMessage, b: ChatMessage) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id);

function Conversation({ sessionId, focus }: { sessionId: string; focus: ChatFocus | null }) {
  const { slug, workspace } = useWorkspace();
  const qc = useQueryClient();
  const pages = useInfiniteQuery(q.messages(slug, sessionId));
  const { data: cards = [] } = useQuery(q.planCards(slug, sessionId));
  const { data: pending } = useQuery(q.pending(slug, sessionId));
  const [local, setLocal] = useState<LocalMessage[]>([]);
  const [unseen, setUnseen] = useState(0);
  const [highlight, setHighlight] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const list = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const atBottom = useRef(true);
  const lastCount = useRef(0);
  const prepend = useRef<{ height: number; top: number } | null>(null);

  // Sent while the super-agent is busy, a message waits in the queue and the
  // messages page leaves it out until its turn: shown from pending-task.
  const queuedIds = useMemo(
    () => new Set((pending?.queued_tasks ?? []).map((t) => t.message_id).filter((id): id is string => !!id)),
    [pending],
  );
  const messages = useMemo(() => {
    const byId = new Map<string, ChatMessage>();
    for (const page of pages.data?.pages ?? []) for (const m of page.messages) byId.set(m.id, m);
    for (const t of pending?.queued_tasks ?? []) {
      if (t.message_id && t.content && !byId.has(t.message_id)) {
        byId.set(t.message_id, { id: t.message_id, chat_session_id: sessionId, role: "user", content: t.content, task_id: t.task_id, created_at: t.created_at });
      }
    }
    return [...byId.values()].sort(byTime);
  }, [pages.data, pending, sessionId]);
  const cardsByMessage = useMemo(() => {
    const m = new Map<string, PlanCard[]>();
    for (const c of cards) m.set(c.chat_message_id, [...(m.get(c.chat_message_id) ?? []), c]);
    return m;
  }, [cards]);

  // ⌘+J / Ctrl+J from anywhere puts the cursor in the chat box (N40).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "j") {
        e.preventDefault();
        input.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Keep the reader's place: at the bottom, follow new messages; scrolled up,
  // count them instead (N44). Older pages keep the view where it was.
  useLayoutEffect(() => {
    const el = list.current;
    if (!el) return;
    const total = messages.length + local.length;
    if (prepend.current) {
      el.scrollTop = prepend.current.top + (el.scrollHeight - prepend.current.height);
      prepend.current = null;
    } else if (total > lastCount.current) {
      // Read now: the updater runs later, after lastCount has moved on.
      const added = total - lastCount.current;
      if (atBottom.current || lastCount.current === 0) el.scrollTop = el.scrollHeight;
      else setUnseen((n) => n + added);
    }
    lastCount.current = total;
  }, [messages.length, local.length]);

  const loadOlder = useCallback(async () => {
    const el = list.current;
    if (!el || !pages.hasNextPage || pages.isFetchingNextPage) return;
    prepend.current = { height: el.scrollHeight, top: el.scrollTop };
    await pages.fetchNextPage();
  }, [pages]);

  const onScroll = () => {
    const el = list.current;
    if (!el) return;
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < AT_BOTTOM;
    if (atBottom.current) setUnseen(0);
    if (el.scrollTop < 60) void loadOlder();
  };

  // /<slug>/chat?session=&message=: page back until the message is there (N46).
  const focusKey = focus ? `${focus.sessionId}:${focus.messageId}` : null;
  const handled = useRef<string | null>(null);
  useEffect(() => {
    if (!focus || !focusKey || handled.current === focusKey || !pages.data) return;
    if (focus.sessionId !== sessionId) {
      handled.current = focusKey;
      setNote("这条消息不在你的对话里");
      return;
    }
    if (!focus.messageId) return;
    if (messages.some((m) => m.id === focus.messageId)) {
      handled.current = focusKey;
      const id = focus.messageId;
      setHighlight(id);
      requestAnimationFrame(() => document.getElementById(`msg-${id}`)?.scrollIntoView({ block: "center" }));
      const t = setTimeout(() => setHighlight(null), 3000);
      return () => clearTimeout(t);
    }
    if (pages.hasNextPage && (pages.data.pages.length ?? 0) < MAX_PAGES) {
      if (!pages.isFetchingNextPage) void pages.fetchNextPage();
    } else {
      handled.current = focusKey;
      setNote("消息太早，没能定位");
    }
    return undefined;
  }, [focus, focusKey, messages, pages, sessionId]);

  const deliver = async (msg: LocalMessage) => {
    setLocal((l) => l.map((m) => (m.key === msg.key ? { ...m, state: "sending" } : m)));
    try {
      if (msg.state === "failed") {
        // The first attempt may have landed and only the answer got lost.
        const newest = await messagesPage(slug, sessionId, null);
        const landed = newest.messages.find((m) => m.role === "user" && m.content.trim() === msg.content.trim() && m.created_at >= msg.since);
        if (landed) {
          mergeMessage(qc, slug, sessionId, landed);
          setLocal((l) => l.filter((m) => m.key !== msg.key));
          return;
        }
      }
      const { message_id } = await sendMessage(slug, sessionId, msg.content);
      mergeMessage(qc, slug, sessionId, {
        id: message_id,
        chat_session_id: sessionId,
        role: "user",
        content: msg.content,
        task_id: null,
        created_at: new Date().toISOString(),
      });
      setLocal((l) => l.filter((m) => m.key !== msg.key));
      void qc.invalidateQueries({ queryKey: keys.pending(slug, sessionId) });
    } catch {
      setLocal((l) => l.map((m) => (m.key === msg.key ? { ...m, state: "failed" } : m)));
    }
  };

  const send = async (text: string): Promise<SendResult> => {
    const msg: LocalMessage = {
      key: `local-${Date.now()}-${Math.random()}`,
      content: text,
      state: "sending",
      since: new Date(Date.now() - 5000).toISOString(),
    };
    atBottom.current = true;
    setLocal((l) => [...l, msg]);
    void deliver(msg);
    return { ok: true };
  };

  const replying = !!pending?.task_id;
  return (
    <aside className="chat" aria-label="对话">
      <h2 className="chat-head">
        <span className="avatar" aria-hidden>s</span>super-agent
      </h2>
      {note && (
        <p className="note" role="status">
          {note} <button type="button" className="link-button" onClick={() => setNote(null)}>知道了</button>
        </p>
      )}
      <div className="messages" ref={list} onScroll={onScroll} data-testid="messages">
        {pages.isFetchingNextPage && <p className="muted center">加载更早的消息…</p>}
        {pages.isError && <p className="error">消息没加载出来</p>}
        {messages.map((m) => (
          <div key={m.id} id={`msg-${m.id}`} data-message-id={m.id} className={`msg ${m.role}${highlight === m.id ? " highlight" : ""}`}>
            <Markdown text={m.content} />
            {queuedIds.has(m.id) && <span className="muted queued">等上一条回复完再处理</span>}
            {(cardsByMessage.get(m.id) ?? []).map((c) => <PlanCardView key={c.id} card={c} sessionId={sessionId} />)}
          </div>
        ))}
        {local.map((m) => (
          <div key={m.key} className={`msg user local ${m.state}`}>
            <p className="plain">{m.content}</p>
            {m.state === "sending" ? (
              <span className="muted">发送中…</span>
            ) : (
              <button type="button" className="link-button error" onClick={() => void deliver(m)}>没发出去，点一下重发</button>
            )}
          </div>
        ))}
        {replying && <p className="muted typing">super-agent 正在回复…</p>}
      </div>
      {unseen > 0 && (
        <button
          type="button"
          className="new-messages"
          onClick={() => {
            const el = list.current;
            if (el) el.scrollTop = el.scrollHeight;
            setUnseen(0);
          }}
        >
          {unseen} 条新消息
        </button>
      )}
      <Composer
        label="对话"
        inputRef={input}
        draftKey={draftKey(workspace.id, "chat")}
        placeholder="跟 super-agent 说，Enter 发送，Shift+Enter 换行"
        onSend={send}
      />
    </aside>
  );
}
