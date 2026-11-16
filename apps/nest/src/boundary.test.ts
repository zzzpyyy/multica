// N60, review requirement 6: Nest does not depend on, import, or copy the
// Multica interface or packages/core. Every source file under apps/nest is
// scanned — vite.config.ts and server.mjs included, not just src.
import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const FORBIDDEN_DIRS = ["apps/web", "apps/desktop", "apps/mobile", "packages/views", "packages/ui", "packages/core"];
const SKIP = new Set(["node_modules", "dist"]);
const SOURCE = /\.(ts|tsx|mts|cts|js|jsx|mjs|cjs)$/;

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    if (SKIP.has(name)) return [];
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) return sourceFiles(full);
    return SOURCE.test(name) ? [full] : [];
  });
}

/** Every module specifier: static and dynamic imports, re-exports, require. */
export function specifiers(code: string): string[] {
  const out: string[] = [];
  const patterns = [
    /\b(?:import|export)\s[^'"`;]*?\bfrom\s*['"]([^'"]+)['"]/g,
    /\bimport\s*['"]([^'"]+)['"]/g,
    /\bimport\s*\(\s*['"`]([^'"`]+)['"`]\s*\)/g,
    /\brequire\s*\(\s*['"`]([^'"`]+)['"`]\s*\)/g,
  ];
  for (const re of patterns) for (const m of code.matchAll(re)) out.push(m[1]!);
  return out;
}

/**
 * A path import must stay inside apps/nest: that keeps it out of the six
 * forbidden directories, and out of anything else in the repository.
 */
function leavesApp(file: string, spec: string): boolean {
  if (!spec.startsWith(".") && !spec.startsWith("/")) return false;
  const abs = path.resolve(path.dirname(file), spec);
  return abs !== ROOT && !abs.startsWith(`${ROOT}${path.sep}`);
}

describe("licence boundary (N60)", () => {
  const files = sourceFiles(ROOT);

  it("scans the whole app, config and server included", () => {
    const rel = files.map((f) => path.relative(ROOT, f));
    expect(rel).toEqual(expect.arrayContaining(["vite.config.ts", "server.mjs", "src/main.tsx", "src/api.ts"]));
  });

  it("package.json has no @multica/ package", () => {
    const pkg = JSON.parse(readFileSync(path.join(ROOT, "package.json"), "utf8")) as Record<string, Record<string, string> | undefined>;
    const deps = ["dependencies", "devDependencies", "peerDependencies", "optionalDependencies"].flatMap((k) =>
      Object.keys(pkg[k] ?? {}),
    );
    expect(deps.filter((d) => d.startsWith("@multica/"))).toEqual([]);
  });

  it(`no import points outside apps/nest (so none at ${FORBIDDEN_DIRS.join(", ")})`, () => {
    const bad: string[] = [];
    for (const file of files) {
      for (const spec of specifiers(readFileSync(file, "utf8"))) {
        const hit = spec.startsWith("@multica/") || leavesApp(file, spec);
        if (hit) bad.push(`${path.relative(ROOT, file)} → ${spec}`);
      }
    }
    expect(bad).toEqual([]);
  });

  it("the scanner itself catches the forms it must", () => {
    // Built at run time, so this file's own text holds no forbidden import.
    const [core, ui] = ["../../packages/" + "core/x", "@" + "multica/ui"];
    const sample = [
      `import a from "${core}";`,
      `export { b } from '${ui}';`,
      `const c = await import("../web/y");`,
      `require("z");`,
      `import "w";`,
    ].join("\n");
    expect(specifiers(sample)).toEqual([core, ui, "w", "../web/y", "z"]);
    expect(leavesApp(path.join(ROOT, "src/a.ts"), core)).toBe(true);
    expect(leavesApp(path.join(ROOT, "src/a.ts"), "./model")).toBe(false);
  });
});
