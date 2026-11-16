// The three message boxes (comment, reply, chat) share this: Enter sends,
// Shift+Enter breaks the line, the input method keeps its Enter (plan 6.5).
import { useEffect, useRef, useState, type RefObject } from "react";
import { keyAction, readDraft, writeDraft } from "../composer";

export type SendResult = { ok: true } | { ok: false; message: string };

export function Composer({
  draftKey,
  placeholder,
  onSend,
  autoFocus = false,
  inputRef,
  label,
}: {
  /** localStorage key: the text survives switching issues and reloading. */
  draftKey: string;
  placeholder: string;
  onSend: (text: string) => Promise<SendResult>;
  autoFocus?: boolean;
  inputRef?: RefObject<HTMLTextAreaElement | null>;
  label: string;
}) {
  const [text, setText] = useState(() => readDraft(draftKey));
  const [error, setError] = useState<string | null>(null);
  const sending = useRef(false);
  const [busy, setBusy] = useState(false);
  const own = useRef<HTMLTextAreaElement>(null);
  const ref = inputRef ?? own;

  // Another issue or reply box: load that one's draft.
  useEffect(() => {
    setText(readDraft(draftKey));
    setError(null);
  }, [draftKey]);

  const update = (value: string) => {
    setText(value);
    writeDraft(draftKey, value);
  };

  const send = async () => {
    const value = ref.current?.value ?? text;
    if (sending.current || value.trim() === "") return;
    sending.current = true;
    setBusy(true);
    setError(null);
    try {
      const result = await onSend(value);
      if (result.ok) {
        // A draft typed meanwhile in another box is untouched: only this key clears.
        writeDraft(draftKey, "");
        setText("");
      } else setError(result.message);
    } finally {
      sending.current = false;
      setBusy(false);
    }
  };

  return (
    <div className="composer">
      <textarea
        ref={ref}
        aria-label={label}
        value={text}
        placeholder={placeholder}
        autoFocus={autoFocus}
        rows={2}
        onChange={(e) => update(e.target.value)}
        onKeyDown={(e) => {
          const action = keyAction(e.nativeEvent, e.currentTarget.value);
          if (action === "send" || action === "swallow") e.preventDefault();
          if (action === "send") void send();
        }}
      />
      <div className="composer-bar">
        {error && <span className="error">{error}</span>}
        <button type="button" disabled={busy || text.trim() === ""} onClick={() => void send()}>
          发送
        </button>
      </div>
    </div>
  );
}
