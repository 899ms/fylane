import { useEffect, useRef } from "react";
import type { Translator } from "../lib/i18n";

/** What the memory page is, drawn once: three moments between you, the AI
 *  and this page, and the three things worth saying. Opens from the empty
 *  state and wears the field sheet's face (board 18) — nothing new. */
export function MemoryHowSheet({
  tr,
  onClose,
}: {
  tr: Translator;
  onClose: () => void;
}) {
  const { t } = tr;
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    box.current?.focus();
  }, []);

  return (
    <>
      <div className="fy-dim" onClick={onClose} />
      <div
        ref={box}
        tabIndex={-1}
        className="fy-sheet fy-mem-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="fy-how-title"
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.stopPropagation();
            onClose();
          }
        }}
      >
        <div className="fy-mem-sheet-head">
          <h2 id="fy-how-title">{t("memory.how.title")}</h2>
        </div>

        <div className="fy-how">
          <div className="fy-how-cols" aria-hidden="true">
            <span>{t("memory.how.you")}</span>
            <span>{t("memory.how.ai")}</span>
            <span>{t("memory.how.here")}</span>
          </div>
          <div className="fy-how-row">
            <Glyph from={2} to={1} />
            <span>{t("memory.how.s1")}</span>
          </div>
          <div className="fy-how-row">
            <Glyph from={1} to={2} />
            <span>{t("memory.how.s2")}</span>
          </div>
          <div className="fy-how-row">
            <Glyph from={0} to={1} />
            <span>{t("memory.how.s3")}</span>
          </div>
        </div>

        <div className="fy-eyebrow">{t("memory.how.sayTitle")}</div>
        <ul className="fy-how-say">
          <li>{t("memory.how.say1")}</li>
          <li>{t("memory.how.say2")}</li>
          <li>{t("memory.how.say3")}</li>
        </ul>
        <p className="fy-how-note">{t("memory.how.note")}</p>

        <div style={{ display: "flex", justifyContent: "flex-end" }}>
          <button type="button" className="fy-mem-link" onClick={onClose}>
            {t("memory.how.ok")}
          </button>
        </div>
      </div>
    </>
  );
}

/** Three points on a line — you, the AI, this page — with one arrow
 *  between two of them. The rest of the row says what the arrow carries. */
function Glyph({ from, to }: { from: 0 | 1 | 2; to: 0 | 1 | 2 }) {
  const xs = [10, 60, 110];
  const x1 = xs[from];
  const x2 = xs[to];
  const dir = x2 > x1 ? 1 : -1;
  const tip = x2 - dir * 6;
  return (
    <svg className="fy-how-glyph" viewBox="0 0 120 20" aria-hidden="true">
      <line x1="10" y1="10" x2="110" y2="10" />
      {xs.map((x, i) => (
        <circle
          key={x}
          cx={x}
          cy="10"
          r="3"
          className={i === from || i === to ? "fy-how-dot" : "fy-how-dot-off"}
        />
      ))}
      <line x1={x1 + dir * 6} y1="10" x2={tip} y2="10" className="fy-how-arrow" />
      <path d={`M${tip - dir * 4} 6 L${tip} 10 L${tip - dir * 4} 14`} className="fy-how-arrow" />
    </svg>
  );
}
