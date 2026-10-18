import { useEffect, useRef, useState } from "react";
import { Check, LoaderCircle, X } from "lucide-react";
import { api } from "./api";
import type { Chapter } from "./types";

export default function ManualTranslationEditor({
  chapter,
  onClose,
  onSaved,
}: {
  chapter: Chapter;
  onClose: () => void;
  onSaved: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [drafts, setDrafts] = useState(() =>
    Object.fromEntries(chapter.segments.map((s) => [s.id, s.translation])),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const dirty = chapter.segments.some((s) => drafts[s.id] !== s.translation);
  const changes = chapter.segments
    .filter((s) => drafts[s.id].trim() && drafts[s.id] !== s.translation)
    .map((s) => ({
      segment_id: s.id,
      text: drafts[s.id],
      revision: s.translation_revision,
    }));
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  useEffect(() => {
    const prevent = (event: BeforeUnloadEvent) => {
      if (dirty || saving) event.preventDefault();
    };
    window.addEventListener("beforeunload", prevent);
    return () => window.removeEventListener("beforeunload", prevent);
  }, [dirty, saving]);
  const close = () => {
    if (saving) return;
    if (dirty && !window.confirm("放弃尚未保存的译文？")) return;
    onClose();
  };
  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await api(`/chapters/${chapter.id}/translations`, "PATCH", {
        revision: chapter.revision,
        items: changes,
      });
      onSaved();
    } catch (e) {
      setError(e instanceof Error ? e.message : "保存失败，请重试");
    } finally {
      setSaving(false);
    }
  };
  return (
    <dialog
      ref={dialog}
      className="modal wide manual-editor"
      aria-labelledby="manual-editor-title"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <header>
        <h2 id="manual-editor-title">手动输入译文</h2>
        <button
          className="icon"
          aria-label="关闭译文编辑"
          disabled={saving}
          onClick={close}
        >
          <X size={20} />
        </button>
      </header>
      <p className="muted">
        按原文段落输入或粘贴中文，可只填写部分段落。留空的段落不作修改；已有译文保持不变。保存不调用翻译模型，也不影响原文和语音。
      </p>
      {error && (
        <p className="manual-error" role="alert">
          {error} 输入内容已保留。
        </p>
      )}
      <div className="manual-rows">
        {chapter.segments.map((segment, i) => (
          <section className="manual-row" key={segment.id}>
            <p className="manual-source" lang="ja">
              <span>{String(i + 1).padStart(2, "0")}</span>
              {segment.text}
            </p>
            <label>
              第 {i + 1} 段中文译文
              <textarea
                aria-label={`第 ${i + 1} 段中文译文`}
                rows={3}
                value={drafts[segment.id]}
                disabled={saving}
                placeholder="可稍后填写"
                onChange={(e) =>
                  setDrafts((old) => ({ ...old, [segment.id]: e.target.value }))
                }
              />
            </label>
          </section>
        ))}
      </div>
      <div className="modal-footer">
        <span className="muted">本次保存 {changes.length} 段</span>
        <button
          type="button"
          className="button"
          disabled={saving || changes.length === 0}
          onClick={() => void save()}
        >
          {saving ? (
            <LoaderCircle className="spin" size={16} />
          ) : (
            <Check size={16} />
          )}
          保存已填写译文
        </button>
      </div>
    </dialog>
  );
}
