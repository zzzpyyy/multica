// Login (PRD v4 section 2): email and code only. The address stays as it
// is, so after signing in the page that was asked for opens (N1).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, sendCode, verifyCode } from "../api";
import { PRODUCT_NAME } from "../config";
import { keys } from "../queries";

export function Login() {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      if (e instanceof ApiError && e.offline) setError("连不上服务器，检查网络后再试");
      else if (e instanceof ApiError && e.status === 429) setError("请求太频繁，等一分钟再试");
      else if (e instanceof ApiError && e.status === 400) setError("验证码不对或已过期");
      else setError(e instanceof ApiError ? e.message : "出错了，再试一次");
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="login">
      <h1>{PRODUCT_NAME}</h1>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!sent) void run(async () => { await sendCode(email.trim()); setSent(true); });
          else void run(async () => { await verifyCode(email.trim(), code.trim()); await qc.invalidateQueries({ queryKey: keys.me }); });
        }}
      >
        <label>
          邮箱
          <input type="email" required autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        {sent && (
          <label>
            验证码
            <input inputMode="numeric" autoComplete="one-time-code" required value={code} onChange={(e) => setCode(e.target.value)} autoFocus />
          </label>
        )}
        <button type="submit" className="primary" disabled={busy}>{sent ? "登录" : "发送验证码"}</button>
        {sent && (
          <button type="button" className="link-button" disabled={busy} onClick={() => void run(async () => { await sendCode(email.trim()); })}>
            重新发送验证码
          </button>
        )}
        {error && <p className="error" role="alert">{error}</p>}
      </form>
    </main>
  );
}
