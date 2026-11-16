import { describe, expect, it } from "vitest";
import { classifyLink, imageSrc } from "./links";

const ORIGIN = "http://localhost:18931";

describe("classifyLink (plan section 5, review requirement 2)", () => {
  it("the three links the server writes open in Nest", () => {
    expect(classifyLink("mention://issue/abc", ORIGIN)).toEqual({ kind: "issue", issueId: "abc" });
    expect(classifyLink("/nest-qa/issues/abc#comment-c1", ORIGIN)).toEqual({ kind: "app", path: "/nest-qa/issues/abc#comment-c1" });
    expect(classifyLink("/nest-qa/chat?session=s&message=m", ORIGIN)).toEqual({ kind: "app", path: "/nest-qa/chat?session=s&message=m" });
  });

  it("names are not links (N23, N36)", () => {
    expect(classifyLink("mention://member/u1", ORIGIN)).toEqual({ kind: "name", actor: "member", id: "u1" });
    expect(classifyLink("mention://squad/s1", ORIGIN)).toEqual({ kind: "name", actor: "squad", id: "s1" });
    expect(classifyLink("mention://project/p1", ORIGIN)).toEqual({ kind: "name", actor: "project", id: "p1" });
    expect(classifyLink("mention://all/all", ORIGIN)).toEqual({ kind: "text" });
  });

  it("other same-origin addresses are files", () => {
    expect(classifyLink("/uploads/a.png", ORIGIN)).toEqual({ kind: "file", href: `${ORIGIN}/uploads/a.png` });
    expect(classifyLink("/api/attachments/1/download", ORIGIN).kind).toBe("file");
  });

  it("// and /\\ resolve to another host: external, never a Nest page", () => {
    expect(classifyLink("//example.com/nest-qa/issues/x", ORIGIN)).toEqual({ kind: "external", href: "http://example.com/nest-qa/issues/x" });
    expect(classifyLink("/\\example.com/y", ORIGIN)).toEqual({ kind: "external", href: "http://example.com/y" });
    expect(classifyLink("https://example.com", ORIGIN)).toEqual({ kind: "external", href: "https://example.com/" });
  });

  it("unsafe schemes are plain text (N24)", () => {
    for (const href of ["javascript:alert(1)", " JavaScript:alert(1)", "data:text/html,x", "vbscript:x", "", "file:///etc/passwd"]) {
      expect(classifyLink(href, ORIGIN)).toEqual({ kind: "text" });
    }
  });
});

describe("imageSrc", () => {
  it("same origin and https only", () => {
    expect(imageSrc("/uploads/a.png", ORIGIN)).toBe(`${ORIGIN}/uploads/a.png`);
    expect(imageSrc("https://cdn.example.com/a.png", ORIGIN)).toBe("https://cdn.example.com/a.png");
    expect(imageSrc("//cdn.example.com/a.png", ORIGIN)).toBeNull();
    expect(imageSrc("http://cdn.example.com/a.png", ORIGIN)).toBeNull();
    expect(imageSrc("javascript:x", ORIGIN)).toBeNull();
  });
});
