// Top bar (PRD v4 section 2 rule 5, layout A): name, workspace switcher, the
// tree's search and "只看要我处理的", logout.
import { useState } from "react";
import type { Workspace } from "../api";
import { PRODUCT_NAME } from "../config";
import { go, issuesPath } from "../router";

/** What the tree shows: titles or identifiers containing `text`, and only what waits on you when `mine`. */
export interface Filter {
  text: string;
  mine: boolean;
}

export function TopBar({
  workspaces,
  current,
  filter,
  onFilter,
  onLogout,
}: {
  workspaces: Workspace[];
  current: Workspace | null;
  filter: Filter | null;
  onFilter: (f: Filter) => void;
  onLogout: () => void;
}) {
  const [menu, setMenu] = useState<"ws" | "account" | null>(null);
  const toggle = (m: "ws" | "account") => setMenu((cur) => (cur === m ? null : m));
  return (
    <header className="topbar">
      <div className="topbar-left">
        <strong className="product">
          <span className="logo" aria-hidden>{Array.from(PRODUCT_NAME)[0]}</span>
          {PRODUCT_NAME}
        </strong>
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
        {filter && (
          <>
            <input
              type="search"
              className="search"
              aria-label="搜索任务"
              placeholder="搜索任务标题或编号"
              value={filter.text}
              onChange={(e) => onFilter({ ...filter, text: e.target.value })}
            />
            <button type="button" className="toggle" aria-pressed={filter.mine} onClick={() => onFilter({ ...filter, mine: !filter.mine })}>
              只看要我处理的
            </button>
          </>
        )}
      </div>
      <div className="topbar-right">
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
