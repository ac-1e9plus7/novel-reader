import { useState } from "react";
import { api } from "./api";

export default function AudioSettings({
  concurrency,
  onSaved,
}: {
  concurrency: number;
  onSaved: (value: number) => void;
}) {
  const [value, setValue] = useState(String(concurrency)),
    [saving, setSaving] = useState(false),
    [error, setError] = useState(""),
    [message, setMessage] = useState("");
  const parsed = Number(value),
    valid = Number.isInteger(parsed) && parsed >= 1 && parsed <= 32;
  return (
    <details className="advanced-settings">
      <summary>高级设置</summary>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (!valid) return;
          setSaving(true);
          setError("");
          setMessage("");
          void api<{ tts_concurrency: number }>("/settings/audio", "PATCH", {
            concurrency: parsed,
          })
            .then((result) => {
              onSaved(result.tts_concurrency);
              setMessage("已保存，后续语音请求立即使用新并发量。");
            })
            .catch((e) => setError(e instanceof Error ? e.message : "保存失败"))
            .finally(() => setSaving(false));
        }}
      >
        <label>
          语音生成并发量
          <input
            type="number"
            min={1}
            max={32}
            step={1}
            required
            value={value}
            disabled={saving}
            onChange={(e) => {
              setValue(e.target.value);
              setMessage("");
            }}
          />
        </label>
        <p className="muted">
          默认 10，范围
          1–32，控制所有章节同时生成语音的请求总数。设置保存在本机，重启后仍生效。调低时已在生成的音频会继续完成；播放速度仍默认为
          1.0。
        </p>
        <button
          className="button secondary"
          type="submit"
          disabled={saving || !valid}
        >
          {saving ? "保存中…" : "保存并发设置"}
        </button>
        {message && (
          <p className="muted" role="status">
            {message}
          </p>
        )}
        {error && (
          <p className="manual-error" role="alert">
            {error}
          </p>
        )}
      </form>
    </details>
  );
}
