// Entry: login check, workspace resolution, the three columns, realtime.
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
  topLevelOf,
} from "./model";
import { keys, q } from "./queries";
import { connect, type EventContext } from "./realtime";
import { go, issuePath, issuesPath, parse, replace, subscribe, type Route } from "./router";
import { ChatPanel } from "./ui/Chat";
import { WorkspaceContext, type WorkspaceData } from "./ui/context";
import { IssueView } from "./ui/IssueView";
import { LeftPanel } from "./ui/LeftPanel";
import { Login } from "./ui/Login";
import { TopBar, type Mode } from "./ui/TopBar";
import { Tree } from "./ui/Tree";
import "./styles.css";

document.title = PRODUCT_NAME;

const LAST_WS = "nest:last-workspace";
const MODE = "nest:mode";

function useRoute(): Route {
  const href = useSyncExternalStore(subscribe, () => location.pathname + location.search + location.hash);
  return useMemo(() => parse(location), [href]); // eslint-disable-line react-hooks/exhaustive-deps
}

function readMode(): Mode {
  try {
    return localStorage.getItem(MODE) === "tree" ? "tree" : "focus";
  } catch {
    return "focus";
  }
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
  const [mode, setMode] = useState<Mode>(readMode);
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
  const changeMode = (m: Mode) => {
    setMode(m);
    try {
      localStorage.setItem(MODE, m);
    } catch {
      // ignore
    }
  };

  if (workspaces.isPending) return <p className="muted center">加载中…</p>;
  if (workspaces.isError) return <p className="error center">工作区没加载出来</p>;
  if (list.length === 0) {
    return (
      <div className="app">
        <TopBar workspaces={[]} current={null} mode={null} onMode={changeMode} onLogout={() => void signOut()} />
        <p className="empty center">你还没有加入任何工作区</p>
      </div>
    );
  }
  if (!current) return null;
  return (
    <div className="app">
      <TopBar workspaces={list} current={current} mode={mode} onMode={changeMode} onLogout={() => void signOut()} />
      <WorkspacePage key={current.slug} workspace={current} me={me} route={route} mode={mode} onMode={changeMode} />
    </div>
  );
}

function WorkspacePage({
  workspace,
  me,
  route,
  mode,
  onMode,
}: {
  workspace: Workspace;
  me: Me;
  route: Route;
  mode: Mode;
  onMode: (m: Mode) => void;
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

  const data = useMemo<Omit<WorkspaceData, "current">>(() => {
    const tree = buildTree(issues.data ?? []);
    return {
      slug,
      workspace,
      userId: me.id,
      tree,
      ready: issues.data !== undefined && (statuses.data !== undefined || statuses.isError),
      cat: catalog(statuses.data ?? []),
      att: attention.data ? indexAttention(attention.data) : EMPTY_ATTENTION,
      runs: snapshot.data ? indexRuns(snapshot.data) : EMPTY_RUNS,
      names: names(members.data ?? [], agents.data ?? [], squads.data ?? [], projects.data ?? []),
    };
  }, [slug, workspace, me.id, issues.data, statuses.data, statuses.isError, attention.data, snapshot.data, members.data, agents.data, squads.data, projects.data]);

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
  const top = current ? topLevelOf(data.tree, current) : null;
  const focus = route.kind === "chat" ? { sessionId: route.sessionId, messageId: route.messageId } : null;

  return (
    <WorkspaceContext.Provider value={value}>
      <div className={`columns mode-${mode}`}>
        {!data.ready ? (
          <p className="muted center">加载任务…</p>
        ) : issues.isError ? (
          <p className="error center">任务没加载出来</p>
        ) : mode === "tree" ? (
          <main className="outline">
            <Tree onOpen={(i) => { onMode("focus"); go(issuePath(slug, i.id)); }} />
          </main>
        ) : (
          <>
            <LeftPanel selectedTopId={top?.id ?? null} />
            <main className="middle">
              {current ? (
                <IssueView key={current.id} issue={current} commentId={route.kind === "issue" ? route.commentId : null} />
              ) : (
                <p className="empty">现在没有进行中的任务，在右边跟 super-agent 说一件事就行</p>
              )}
            </main>
          </>
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
