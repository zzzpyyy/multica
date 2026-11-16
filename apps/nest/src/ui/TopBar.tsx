// Top bar (PRD v4 section 2 rule 5): name, workspace switcher, focus/tree, logout.
import { useState } from "react";
import type { Workspace } from "../api";
import { PRODUCT_NAME } from "../config";
import { go, issuesPath } from "../router";

export type Mode = "focus" | "tree";

export function TopBar({
  workspaces,
  current,
  mode,
  onMode,
  onLogout,
}: {
  workspaces: Workspace[];
  current: Workspace | null;
  mode: Mode | null;
  onMode: (m: Mode) => void;
  onLogout: () => void;
}) {
  const [menu, setMenu] = useState<"ws" | "account" | null>(null);
  const toggle = (m: "ws" | "account") => setMenu((cur) => (cur === m ? null : m));
  return (
    <header className="topbar">
      <div className="topbar-left">
        <strong className="product">{PRODUCT_NAME}</strong>
        {current && (
          <div className="menu">
            <button type="button" aria-haspopup="menu" aria-expanded={menu === "ws"} onClick={() => toggle("ws")}>
              {current.name} ▾
            </button>
            {menu === "ws" && (
              <ul role="menu" className="menu-list">
                {workspaces.map((w) => (
                  <li key={w.id} role="none">
                    <button
                      type="button"
                      role="menuitem"
                      aria-current={w.id === current.id ? "true" : undefined}
                      onClick={() => { setMenu(null); go(issuesPath(w.slug)); }}
                    >
                      {w.name}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
      <div className="topbar-right">
        {mode && (
          <div className="segmented" role="group" aria-label="模式">
            <button type="button" aria-pressed={mode === "focus"} onClick={() => onMode("focus")}>聚焦</button>
            <button type="button" aria-pressed={mode === "tree"} onClick={() => onMode("tree")}>树形</button>
          </div>
        )}
        <div className="menu">
          <button type="button" aria-haspopup="menu" aria-expanded={menu === "account"} onClick={() => toggle("account")}>账号 ▾</button>
          {menu === "account" && (
            <ul role="menu" className="menu-list right">
              <li role="none">
                <button type="button" role="menuitem" onClick={() => { setMenu(null); onLogout(); }}>退出登录</button>
              </li>
            </ul>
          )}
        </div>
      </div>
    </header>
  );
}
