#!/usr/bin/env node
// D-group acceptance (fork, role instances) driven only through the CLI, as
// the plan asks: D1–D12, D14, D15. Run against a test environment, never a
// production server: it creates issues and agent runs.
//
//   node scripts/fork/d-acceptance.mjs <case>...     cases: d1-4 d5-8 d9 d10 d11 d12 d14 d15 | all
//
// Environment:
//   MULTICA   CLI command, split on spaces (default "multica"); point it at the
//             test server with --profile, e.g. MULTICA="multica --profile test"
//   TB, TL    agent names (default TB, TL), both on one Claude runtime
//   SLEEP     seconds each TB run sleeps (default 60)
//   RUNTIME   runtime id of TB (for D9); read from TB when unset
//
// Each case prints PASS/FAIL lines with the evidence it checked; the exit code
// is the number of failed checks.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

const CLI = (process.env.MULTICA || "multica").split(/\s+/).filter(Boolean);
const TB = process.env.TB || "TB";
const TL = process.env.TL || "TL";
const SLEEP = Number(process.env.SLEEP || 60);
let failures = 0;

const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
const check = (id, ok, evidence) => {
  if (!ok) failures++;
  log(`${ok ? "PASS" : "FAIL"} ${id}: ${evidence}`);
};
const sleep = (s) => new Promise((r) => setTimeout(r, s * 1000));

function cli(args, { allowFail = false } = {}) {
  try {
    return execFileSync(CLI[0], [...CLI.slice(1), ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  } catch (e) {
    if (allowFail) return { error: `${e.stderr || ""}${e.stdout || ""}`.trim() };
    throw new Error(`${args.join(" ")}: ${e.stderr || e.message}`);
  }
}
const json = (args) => JSON.parse(cli([...args, "--output", "json"]));
const list = (v, key) => (Array.isArray(v) ? v : v?.[key] ?? []);

const agents = new Map(list(json(["agent", "list"]), "agents").map((a) => [a.name, a]));
const agentID = (name) => {
  const a = agents.get(name);
  if (!a) throw new Error(`agent ${name} not found`);
  return a.id;
};

function newIssue(title, extra = []) {
  const i = json(["issue", "create", "--title", `[D] ${title} ${Date.now() % 100000}`, "--assignee", TL, "--status", "backlog", ...extra]);
  log(`issue ${i.identifier} = ${title}`);
  return i;
}
const add = (issue, splits, extra = []) =>
  json(["issue", "add-instances", issue.id, "--agent", TB, ...splits.flatMap((s) => ["--split", s]), ...extra]);
const batches = (issue) => json(["issue", "instances", issue.id]).batches;
const runs = (issue) => list(json(["issue", "runs", issue.id]), "runs");
const comments = (issue) => list(json(["issue", "comment", "list", issue.id]), "comments");
const cancel = (taskID) => cli(["issue", "cancel-task", taskID], { allowFail: true });
const statusOf = (issue) => batches(issue).flatMap((b) => b.instances.map((i) => i.status));
const count = (xs, v) => xs.filter((x) => x === v).length;

async function waitFor(label, seconds, fn) {
  const deadline = Date.now() + seconds * 1000;
  for (;;) {
    const v = fn();
    if (v) return v;
    if (Date.now() > deadline) {
      log(`timeout after ${seconds}s waiting for ${label}`);
      return null;
    }
    await sleep(3);
  }
}

function setupAgents() {
  cli(["agent", "update", agentID(TB), "--max-concurrent-tasks", "3", "--instructions",
    `你是测试角色 ${TB}。只做这几步：1) 如果本次运行有分工说明，把分工说明原样写进当前工作目录下的 part.txt；` +
    `2) 执行 sleep ${SLEEP}；3) 用 multica issue comment add 在当前任务上发一条评论，内容是“完成：<分工说明>”，没有分工说明就写“完成：主任务”；4) 结束。不要读代码，不要做别的事。`]);
  cli(["agent", "update", agentID(TL), "--max-concurrent-tasks", "3", "--instructions",
    `你是测试 leader ${TL}。按顺序判断，命中一条就做完结束：` +
    `1) 触发你的评论是以“加开的”开头的系统汇总评论：数一下里面有几个实例，在任务上发评论“收到 N 条实例结果”（N 换成数字）。` +
    `2) 任务描述里有一行以“加开：”开头：原样执行冒号后面的命令，把命令的完整输出（包括报错）发成任务上的一条评论。` +
    `3) 其他情况什么都不做。`]);
  log(`agents ready: ${TB} sleeps ${SLEEP}s`);
}

function cancelAll() {
  const active = list(json(["agent", "tasks", agentID(TB)]), "tasks").filter((t) => ["queued", "dispatched", "running"].includes(t.status));
  for (const t of active) cancel(t.id);
}

const cases = {
  async "d1-4"() {
    const x = newIssue("X D1-D4");
    const t0 = Date.now();
    add(x, ["a", "b", "c"]);
    const ok = await waitFor("3 running", 60, () => count(statusOf(x), "running") === 3);
    check("D1", ok, `3 instances running after ${Math.round((Date.now() - t0) / 1000)}s (limit 60s)`);
    const inst = await waitFor("sessions", 60, () => {
      const i = batches(x)[0].instances;
      return i.every((v) => v.session_id) ? i : null;
    });
    const done = await waitFor("all ended", SLEEP + 240, () => {
      const i = batches(x)[0];
      return i.instances.every((v) => ["completed", "failed", "cancelled"].includes(v.status)) ? i : null;
    });
    if (inst) {
      const sessions = new Set(inst.map((i) => i.session_id)), dirs = new Set(inst.map((i) => i.work_dir));
      const parts = inst.map((i) => (i.work_dir && existsSync(join(i.work_dir, "part.txt")) ? readFileSync(join(i.work_dir, "part.txt"), "utf8").trim() : "?"));
      check("D2", sessions.size === 3 && dirs.size === 3 && parts.join() === "a,b,c",
        `sessions ${[...sessions].map((s) => s.slice(0, 8))} (while running), workdirs distinct=${dirs.size === 3}, part.txt=${parts} (want a,b,c; ? = workdir not on this machine)`);
    } else check("D2", false, "session ids never reported");
    const lastEnd = done ? Math.max(...done.instances.map((i) => Date.parse(i.completed_at))) : 0;
    const tlRun = await waitFor("TL woken", 150, () => runs(x).find((r) => r.agent_id === agentID(TL) && r.status === "completed"));
    const all = runs(x).filter((r) => r.agent_id === agentID(TL));
    const cs = comments(x);
    const ids = new Set(done?.instances.flatMap((i) => i.task_ids) ?? []);
    const results = cs.filter((c) => ids.has(c.source_task_id) && /完成：[abc]/.test(c.content));
    const summary = cs.find((c) => c.author_type === "system" && c.content.startsWith("加开的"));
    check("D3", results.length === 3 && all.length === 1 && Date.parse(all[0].created_at) >= lastEnd - 1000 &&
      summary && (summary.content.match(/#comment-/g) || []).length >= 3 && all[0].trigger_comment_id === summary.id &&
      cs.some((c) => c.author_id === agentID(TL) && /收到\s*3/.test(c.content)),
      `${results.length} result comments, TL runs=${all.length} (created ${all[0]?.created_at}, last instance ended ${new Date(lastEnd).toISOString()}), summary links=${(summary?.content.match(/#comment-/g) || []).length}, TL replied "收到 3"=${cs.some((c) => c.author_id === agentID(TL) && /收到\s*3/.test(c.content))}`);
    const acts = json(["issue", "timeline", x.id, "--activity-only"]).filter((a) => a.actor_id === agentID(TB) &&
      ["status_changed", "assignee_changed"].includes(a.action));
    const now = json(["issue", "get", x.id]);
    check("D4", acts.length === 0 && now.status === "backlog" && now.assignee_id === agentID(TL) && !now.parent_issue_id,
      `TB status/assignee activities=${acts.length}, issue still backlog/TL/no parent`);
    void tlRun;
  },

  async "d5-8"() {
    for (const priority of [false, true]) {
      const tag = priority ? "D7" : "D6";
      const y = newIssue(`Y ${tag}`), x = newIssue(`X ${tag}`), z = newIssue(`Z ${tag}`);
      add(y, ["y"]);
      await waitFor("Y running", 60, () => count(statusOf(y), "running") === 1);
      add(x, ["x1", "x2", "x3"]);
      await waitFor("X 2 running", 60, () => count(statusOf(x), "running") === 2);
      if (!priority) {
        const b = batches(x)[0], q = b.instances.find((i) => i.status === "queued")?.queue;
        check("D5", count(statusOf(x), "running") === 2 && count(statusOf(x), "queued") === 1 && q?.position === 1 &&
          q?.reason === "role_full" && q?.running === 3 && q?.limit === 3,
          `X running=${count(statusOf(x), "running")} queued=${count(statusOf(x), "queued")}, queue=${JSON.stringify(q)}`);
      }
      cancel(batches(y)[0].instances[0].task_id); // free Y so X holds all three
      await waitFor("X 3 started", 60, () => batches(x)[0].instances.every((i) => i.started_at));
      const before = Object.fromEntries(batches(x)[0].instances.map((i) => [i.task_id, i.started_at]));
      add(x, ["x4"], priority ? ["--priority"] : []);
      await sleep(10);
      add(z, ["z1"]);
      // The first of X4/Z1 to start took the first freed slot; a second slot
      // freeing seconds later goes to the other one, which is fine.
      const first = await waitFor("X4 or Z1 to start", SLEEP + 240, () => {
        const a = batches(x)[0].instances[0], b = batches(z)[0].instances[0];
        if (!a.started_at && !b.started_at) return null;
        return { x4: a, z1: b, winner: !b.started_at || (a.started_at && a.started_at < b.started_at) ? "X4" : "Z1" };
      });
      const firstEnd = batches(x).at(-1).instances.map((i) => i.completed_at).filter(Boolean).sort()[0];
      const ev = `first X instance ended ${firstEnd?.slice(11, 23)}, X4 started ${first?.x4.started_at?.slice(11, 23) ?? "-"}, Z1 started ${first?.z1.started_at?.slice(11, 23) ?? "-"}`;
      if (priority) {
        check("D7", first?.winner === "X4", `freed slot → ${first?.winner} (want X4); ${ev}`);
      } else {
        const x4q = first?.x4.queue;
        check("D6", first?.winner === "Z1" && x4q?.position === 1, `freed slot → ${first?.winner} (want Z1); X4 then still queued at position ${x4q?.position} (want 1); ${ev}`);
      }
      const orig = batches(x).at(-1).instances;
      check(`D8 (${tag})`, orig.every((i) => i.status !== "cancelled" && i.started_at === before[i.task_id]),
        `original X instances: ${orig.map((i) => `${i.index}:${i.status}`).join(" ")}, start times unchanged=${orig.every((i) => i.started_at === before[i.task_id])}`);
      cancelAll();
      await waitFor("TB idle", 60, () => [x, y, z].every((i) => !statusOf(i).some((s) => s === "running" || s === "queued")));
    }
  },

  async d9() {
    cancelAll();
    const rt = process.env.RUNTIME || agents.get(TB).runtime_id;
    cli(["runtime", "concurrency", rt, "1"]);
    try {
      const w = newIssue("W D9");
      add(w, ["w1", "w2"]);
      await waitFor("1 running", 60, () => count(statusOf(w), "running") === 1);
      await sleep(5);
      const q = batches(w)[0].instances.find((i) => i.status === "queued")?.queue;
      check("D9", count(statusOf(w), "running") === 1 && q?.reason === "runtime_full" && q?.limit === 1,
        `running=${count(statusOf(w), "running")}, queued reason=${q?.reason} (${q?.running}/${q?.limit})`);
    } finally {
      cli(["runtime", "concurrency", rt, "none"]);
      cancelAll();
    }
  },

  async d10() {
    const x = newIssue("X D10");
    add(x, ["p", "q", "r"]);
    await waitFor("3 running", 60, () => count(statusOf(x), "running") === 3);
    cancel(batches(x)[0].instances[1].task_id);
    const done = await waitFor("1 and 3 end", SLEEP + 240, () => {
      const s = statusOf(x);
      return s[0] === "completed" && s[2] === "completed" ? s : null;
    });
    await waitFor("TL woken", 150, () => runs(x).find((r) => r.agent_id === agentID(TL)));
    await sleep(20);
    const tl = runs(x).filter((r) => r.agent_id === agentID(TL));
    const summary = comments(x).find((c) => c.author_type === "system" && c.content.startsWith("加开的"));
    check("D10", done && tl.length === 1 && /实例 2\/3[^\n]*已取消/.test(summary?.content || ""),
      `statuses=${statusOf(x)}, TL runs=${tl.length}, summary marks instance 2 cancelled=${/实例 2\/3[^\n]*已取消/.test(summary?.content || "")}`);
  },

  async d11() {
    const x = newIssue("X D11");
    const tooMany = cli(["issue", "add-instances", x.id, "--agent", TB, "--split", "1", "--split", "2", "--split", "3", "--split", "4"], { allowFail: true });
    const empty = cli(["issue", "add-instances", x.id, "--agent", TB, "--split", " "], { allowFail: true });
    check("D11", tooMany.error && empty.error && runs(x).length === 0,
      `k=4: "${String(tooMany.error).split("\n")[0]}"; empty split: "${String(empty.error).split("\n")[0]}"; runs=${runs(x).length}`);
  },

  async d12() {
    const w = json(["issue", "create", "--title", `[D] W D12 ${Date.now() % 100000}`, "--assignee", TB, "--status", "todo"]);
    log(`issue ${w.identifier} = W D12 (assigned to ${TB}, todo)`);
    await waitFor("main run", 60, () => runs(w).some((r) => r.status === "running"));
    cli(["issue", "comment", "add", w.id, "--content", `[@${TB}](mention://agent/${agentID(TB)}) 再跑一次`]);
    await waitFor("both runs end", 2 * SLEEP + 300, () => {
      const r = runs(w).filter((v) => v.agent_id === agentID(TB));
      return r.length >= 2 && r.every((v) => ["completed", "failed", "cancelled"].includes(v.status)) ? r : null;
    });
    const r = runs(w).filter((v) => v.agent_id === agentID(TB) && v.started_at).sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at));
    const overlap = r.some((v, i) => i > 0 && Date.parse(v.started_at) < Date.parse(r[i - 1].completed_at));
    check("D12", r.length >= 2 && !overlap, r.map((v) => `${v.started_at.slice(11, 19)}–${(v.completed_at || "").slice(11, 19)}`).join(", "));
  },

  async d14() {
    const x = newIssue("X D14");
    cli(["issue", "update", x.id, "--description", `加开：multica issue add-instances ${x.identifier} --agent ${TB} --split d --split e`]);
    cli(["issue", "status", x.id, "todo"]);
    const b = await waitFor("TL batch", 240, () => batches(x)[0]);
    const done = await waitFor("instances end", SLEEP + 300, () => {
      const s = statusOf(x);
      return s.length === 2 && s.every((v) => v === "completed") ? s : null;
    });
    const merge = await waitFor("TL woken by summary", 150, () => {
      const summary = comments(x).find((c) => c.author_type === "system" && c.content.startsWith("加开的"));
      return summary && runs(x).find((r) => r.agent_id === agentID(TL) && r.trigger_comment_id === summary.id);
    });
    check("D14", b?.requested_by?.type === "agent" && b?.requested_by?.id === agentID(TL) && done && merge,
      `batch requested_by=${b?.requested_by?.type}:${b?.requested_by?.name}, instances=${done}, TL woken by the summary=${Boolean(merge)}`);
  },

  async d15() {
    const x = newIssue("X D15");
    cli(["issue", "update", x.id, "--description", `加开：multica issue add-instances ${x.identifier} --agent ${TB} --split f --priority`]);
    cli(["issue", "status", x.id, "todo"]);
    const reply = await waitFor("TL reports the error", 240, () =>
      comments(x).find((c) => c.author_id === agentID(TL) && /priority_requires_member/.test(c.content)));
    await sleep(5);
    check("D15", reply && batches(x).length === 0 && !runs(x).some((r) => r.agent_id === agentID(TB)),
      `TL saw: "${(reply?.content || "").replace(/\s+/g, " ").slice(0, 160)}"; batches=${batches(x).length}`);
  },
};

const wanted = process.argv.slice(2);
const order = wanted.includes("all") || wanted.length === 0 ? Object.keys(cases) : wanted;
setupAgents();
for (const name of order) {
  if (!cases[name]) throw new Error(`unknown case ${name}`);
  log(`== ${name}`);
  try {
    await cases[name]();
  } catch (e) {
    check(name, false, e.message);
  }
}
log(`${failures} failed`);
process.exit(failures);
