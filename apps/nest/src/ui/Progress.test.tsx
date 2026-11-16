import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { lineText, type Part } from "../model";
import { Dotted, ProgressLine } from "./Progress";

// BUNN-31 (一鸣 11-16): no line may start with "·", and a folded comment shows
// exactly 20 whole lines. jsdom-free: the markup and the CSS that make it so;
// the real line boxes are measured in a browser on S00.
const text = (html: string) => html.replace(/<[^>]+>/g, "").replace(/&amp;/g, "&");
const parts: Part[] = [
  { text: "阶段 2/3", callToAction: false },
  { text: "审核中：接口联调", callToAction: false },
  { text: "1 个阻塞", callToAction: false },
  { text: "1 项待你验收", callToAction: true },
];

describe("separators", () => {
  it("the one-line progress glues each \" ·\" to the end of the part before it", () => {
    const html = renderToStaticMarkup(<ProgressLine parts={parts} />);
    const spans = [...html.matchAll(/<span class="(part(?: cta)?)">(.*?)<\/span>(?= |<\/p>)/g)].map((m) => [m[1], text(m[2]!)]);
    expect(spans).toEqual([
      ["part", "阶段 2/3 ·"],
      ["part", "审核中：接口联调 ·"],
      ["part", "1 个阻塞 ·"],
      ["part cta", "1 项待你验收"],
    ]);
    expect(text(html)).toBe(lineText(parts));
  });

  it("run counts and folded stages glue it too, text unchanged", () => {
    const html = renderToStaticMarkup(<Dotted text="TB 运行 2 · 排队 1" />);
    expect(html).toBe('TB 运行 2<span class="sep"> ·</span> 排队 1');
    expect(text(renderToStaticMarkup(<Dotted text="阶段 1–13 · 已完成 13 个" />))).toBe("阶段 1–13 · 已完成 13 个");
  });

  it("the CSS keeps a glued \" ·\" whole and lets a part wider than the line wrap inside", () => {
    const css = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
    expect(css).toMatch(/\n\.sep \{ white-space: nowrap; \}/);
    expect(css).toMatch(/\.oneline \.part \{ display: inline-block; max-width: 100%;/);
    expect(css).not.toMatch(/\.oneline \.part \{[^}]*nowrap/);
  });
});

describe("folded comment", () => {
  it("clamps by line count, not by a height that cuts a line in half", () => {
    const css = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
    const rule = css.match(/\.comment-body\.clamp \{([^}]*)\}/)?.[1] ?? "";
    expect(rule).toMatch(/-webkit-line-clamp: 20;/);
    expect(rule).not.toMatch(/max-height/);
  });
});
