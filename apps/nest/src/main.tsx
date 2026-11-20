// Entry: login check, workspace resolution, the tree and the chat (layout A), realtime.
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from "@tanstack/react-query";
import { StrictMode, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { createRoot } from "react-dom/client";
import { ApiError, logout, setUnauthorizedHandler, type Me, type Workspace } from "./api";
import { PRODUCT_NAME } from "./config";
import {
  buildTree,
  catalog,
  defaultIssue,
  EMPTY_ATTENTION,
  EMPTY_RUNS,
  indexAttention,
  indexRuns,
  leftGroups,
  names,
} from "./model";
import { keys, pageState, q } from "./queries";
import { connect, type EventContext } from "./realtime";
import { issuePath, issuesPath, parse, replace, subscribe, type Route } from "./router";
import { ChatPanel } from "./ui/Chat";
import { WorkspaceContext, type WorkspaceData } from "./ui/context";
import { Board } from "./ui/Board";
import { Login } from "./ui/Login";
import { TopBar, type Filter } from "./ui/TopBar";
import "./styles.css";

document.title = PRODUCT_NAME;

const LAST_WS = "nest:last-workspace";
const NO_FILTER: Filter = { text: "", mine: false };

function useRoute(): Route {
  const href = useSyncExternalStore(subscribe, () => location.pathname + location.search + location.hash);
  return useMemo(() => parse(location), [href]); // eslint-disable-line react-hooks/exhaustive-deps
}

function App({ onSignedOut }: { onSignedOut: () => void }) {
  const me = useQuery(q.me());
  if (me.isPending) return <p className="muted center">加载中…</p>;
  if (me.isError) {
    if (me.error instanceof ApiError && me.error.status === 401) return <Login />;
    return (
      <p className="error center">
        连不上服务器 <button type="button" onClick={() => void me.refetch()}>重试</button>
      </p>
    );
  }
  return <SignedIn me={me.data} onSignedOut={onSignedOut} />;
}

function SignedIn({ me, onSignedOut }: { me: Me; onSignedOut: () => void }) {
  const route = useRoute();
  const workspaces = useQuery(q.workspaces());
  const [filter, setFilter] = useState<Filter>(NO_FILTER);
  const list = workspaces.data ?? [];
  const slug = route.kind === "root" ? null : route.slug;
  const current = slug ? list.find((w) => w.slug === slug) ?? null : null;

  // `/`, or a workspace you are not in: go to the last one used, else the first.
  useEffect(() => {
    if (!workspaces.data || list.length === 0) return;
    if (route.kind === "root" || !current) {
      let last: string | null = null;
      try {
        last = localStorage.getItem(LAST_WS);
      } catch {
        last = null;
      }
      const target = list.find((w) => w.slug === last) ?? list[0]!;
      replace(issuesPath(target.slug));
    }
  }, [route.kind, current, workspaces.data, list]);

  useEffect(() => {
    if (!current) return;
    setFilter(NO_FILTER);
    try {
      localStorage.setItem(LAST_WS, current.slug);
    } catch {
      // ignore
    }
  }, [current]);

  const signOut = async () => {
    try {
      await logout();
    } finally {
      onSignedOut();
    }
  };

  if (workspaces.isPending) return <p className="muted center">加载中…</p>;
  if (workspaces.isError) return <p className="error center">工作区没加载出来</p>;
  if (list.length === 0) {
    return (
      <div className="app">
        <TopBar workspaces={[]} current={null} filter={null} onFilter={setFilter} onLogout={() => void signOut()} />
        <p className="empty center">你还没有加入任何工作区</p>
      </div>
    );
  }
  if (!current) return null;
  return (
    <div className="app">
      <TopBar workspaces={list} current={current} filter={filter} onFilter={setFilter} onLogout={() => void signOut()} />
      <WorkspacePage key={current.slug} workspace={current} me={me} route={route} filter={filter} />
    </div>
  );
}

function WorkspacePage({
  workspace,
  me,
  route,
  filter,
}: {
  workspace: Workspace;
  me: Me;
  route: Route;
  filter: Filter;
}) {
  const slug = workspace.slug;
  const qc = useQueryClient();
  const issues = useQuery(q.issues(slug));
  const statuses = useQuery(q.statuses(slug));
  const attention = useQuery(q.attention(slug));
  const snapshot = useQuery(q.snapshot(slug));
  const members = useQuery(q.members(slug, workspace.id));
  const agents = useQuery(q.agents(slug));
  const squads = useQuery(q.squads(slug));
  const projects = useQuery(q.projects(slug));
  const superAgent = useQuery(q.superAgent(slug));

  // Grouping, the picked issue and "在等谁" wait for everything they read (N10).
  const state = pageState({ issues, statuses, attention, snapshot, names: [members, agents, squads] });
  const ready = state.kind === "ready";

  const data = useMemo<Omit<WorkspaceData, "current">>(() => {
    const tree = buildTree(issues.data ?? []);
    return {
      slug,
      workspace,
      userId: me.id,
      tree,
      ready,
      cat: catalog(statuses.data ?? []),
      att: attention.data ? indexAttention(attention.data) : EMPTY_ATTENTION,
      runs: snapshot.data ? indexRuns(snapshot.data) : EMPTY_RUNS,
      names: names(members.data ?? [], agents.data ?? [], squads.data ?? [], projects.data ?? []),
    };
  }, [slug, workspace, me.id, ready, issues.data, statuses.data, attention.data, snapshot.data, members.data, agents.data, squads.data, projects.data]);

  // One /ws connection per workspace page; the context it reads is always current.
  const ctxRef = useRef<EventContext>({ slug, sessionId: null, userId: me.id, onRemovedFromWorkspace: () => replace("/") });
  ctxRef.current = { ...ctxRef.current, sessionId: superAgent.data?.sessionId ?? null };
  useEffect(() => connect(qc, () => ctxRef.current, slug), [qc, slug]);

  const groups = leftGroups(data.tree, data);
  const fallback = defaultIssue(groups);
  let current = null;
  if (route.kind === "issue") current = data.tree.byId.get(route.issueId) ?? null;
  else if (route.kind === "chat") current = fallback;

  // /<slug>/issues picks an issue; unknown addresses and unknown issues go to /<slug>/issues (N7).
  useEffect(() => {
    if (!data.ready) return;
    if (route.kind === "unknown") replace(issuesPath(slug));
    else if (route.kind === "issue" && !data.tree.byId.has(route.issueId)) replace(issuesPath(slug));
    else if (route.kind === "issues" && fallback) replace(issuePath(slug, fallback.id));
  }, [data.ready, data.tree, route, slug, fallback]);

  const value: WorkspaceData = { ...data, current };
  const focus = route.kind === "chat" ? { sessionId: route.sessionId, messageId: route.messageId } : null;

  return (
    <WorkspaceContext.Provider value={value}>
      <div className="columns">
        {state.kind === "failed" ? (
          <div className="error center span-issues" role="alert">
            <p>{state.what.join("、")}没加载出来，先不列任务，免得漏掉等你处理的。</p>
            <button
              type="button"
              onClick={() => {
                for (const r of [issues, attention, snapshot]) if (r.isError) void r.refetch();
              }}
            >
              重试
            </button>
          </div>
        ) : state.kind === "loading" ? (
          <p className="muted center span-issues">加载任务…</p>
        ) : (
          <main className="board" aria-label="任务">
            <Board current={current} commentId={route.kind === "issue" ? route.commentId : null} filter={filter} />
          </main>
        )}
        <ChatPanel focus={focus} />
      </div>
    </WorkspaceContext.Provider>
  );
}

function Root() {
  // A sign-out, or a 401 while signed in, drops every cached query and
  // remounts: nothing of the old session stays on screen (N4).
  const [session, setSession] = useState(0);
  const client = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            // Freshness comes from /ws events and the reconnect refetch.
            staleTime: Infinity,
            refetchOnWindowFocus: false,
            retry: (count, e) => !(e instanceof ApiError && e.status >= 400 && e.status < 500) && count < 2,
          },
        },
      }),
    [session],
  );
  useEffect(() => {
    setUnauthorizedHandler(() => {
      if (client.getQueryData(keys.me)) {
        client.clear();
        setSession((n) => n + 1);
      }
    });
  }, [client]);
  return (
    <QueryClientProvider client={client}>
      <App
        key={session}
        onSignedOut={() => {
          client.clear();
          setSession((n) => n + 1);
        }}
      />
    </QueryClientProvider>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
