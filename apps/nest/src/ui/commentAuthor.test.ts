import { describe, expect, it } from "vitest";
import { names } from "../model";
import { commentAuthor } from "./Comments";

// BUNN-31 (一鸣 11-17): only a member's comment reads 成员; the platform's own
// comments (author_type "system", e.g. the instance batch summary) read 系统.
const n = names(
  [{ user_id: "u1", name: "zhangpengyun", email: "z@example.test" }],
  [{ id: "a-tl", name: "TL" }, { id: "a-super", name: "super-agent", system_key: "super" }],
  [],
  [],
);

describe("commentAuthor", () => {
  it("tells the four authors apart", () => {
    expect(commentAuthor({ author_type: "member", author_id: "u1" }, n)).toEqual({ who: "zhangpengyun", kind: "成员", badge: "member" });
    expect(commentAuthor({ author_type: "agent", author_id: "a-tl" }, n)).toEqual({ who: "TL", kind: "智能体", badge: "agent" });
    expect(commentAuthor({ author_type: "agent", author_id: "a-super" }, n)).toEqual({ who: "super-agent", kind: "super-agent", badge: "super" });
    expect(commentAuthor({ author_type: "system", author_id: "00000000-0000-0000-0000-000000000000" }, n)).toEqual({ who: "系统", kind: "系统", badge: "system" });
  });

  it("a system comment never borrows a member's name, even one with the same id", () => {
    expect(commentAuthor({ author_type: "system", author_id: "u1" }, n).who).toBe("系统");
  });

  it("unknown members and agents fall back to their kind", () => {
    expect(commentAuthor({ author_type: "member", author_id: "gone" }, n).who).toBe("成员");
    expect(commentAuthor({ author_type: "agent", author_id: "gone" }, n).who).toBe("智能体");
  });
});
