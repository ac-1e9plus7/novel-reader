import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  BookOpen,
  Plus,
  ArrowLeft,
  Play,
  Pause,
  SkipForward,
  SkipBack,
  Volume2,
  Settings,
  Sun,
  Moon,
  Library,
  Languages,
  Headphones,
  Check,
  LoaderCircle,
  Pencil,
  X,
  List,
  Repeat,
  Bookmark,
  ChevronRight,
  Trash2,
} from "lucide-react";
import { api, ApiError, audioURL } from "./api";
import type {
  Book,
  Chapter,
  Segment,
  Clip,
  Job,
  Progress,
  Glossary,
  Config,
} from "./types";

const active = (j: Job) => ["queued", "running"].includes(j.status);
const states: Record<string, string> = {
  queued: "排队中",
  running: "进行中",
  succeeded: "已完成",
  failed: "失败",
  partial_failed: "部分完成",
  cancelled: "已停止",
  interrupted: "已中断",
};
const formatTime = (n: number) =>
  `${Math.floor(n / 60)}:${String(Math.floor(n % 60)).padStart(2, "0")}`;
function Modal({
  title,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className={wide ? "modal wide" : "modal"}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <header>
        <h2>{title}</h2>
        <button className="icon" aria-label="关闭" onClick={onClose}>
          <X size={20} />
        </button>
      </header>
      {children}
    </dialog>
  );
}
function Button({
  children,
  onClick,
  disabled = false,
  secondary = false,
}: {
  children: ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  secondary?: boolean;
}) {
  return (
    <button
      type="button"
      className={secondary ? "button secondary" : "button"}
      onClick={onClick}
      disabled={disabled}
    >
      {children}
    </button>
  );
}
export default function App() {
  const [books, setBooks] = useState<Book[]>([]),
    [book, setBook] = useState<Book | null>(null),
    [chapters, setChapters] = useState<Chapter[]>([]),
    [chapter, setChapter] = useState<Chapter | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]),
    [config, setConfig] = useState<Config | null>(null),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const [modal, setModal] = useState<
      "book" | "import" | "source" | "settings" | "glossary" | null
    >(null),
    [edit, setEdit] = useState<{
      segment: Segment;
      kind: "translation" | "speech";
    } | null>(null),
    [detail, setDetail] = useState<Job | null>(null),
    [attempts, setAttempts] = useState<unknown[]>([]);
  const [title, setTitle] = useState(""),
    [author, setAuthor] = useState(""),
    [raw, setRaw] = useState(""),
    [draft, setDraft] = useState(""),
    [glossary, setGlossary] = useState<Glossary>({ revision: 1, terms: [] });
  const [mode, setMode] = useState(
      localStorage.getItem("reader.mode") || "parallel",
    ),
    [theme, setTheme] = useState(
      localStorage.getItem("reader.theme") || "light",
    ),
    [font, setFont] = useState(
      Number(localStorage.getItem("reader.font") || 19),
    ),
    [sidebar, setSidebar] = useState(false);
  const [current, setCurrent] = useState(""),
    [playing, setPlaying] = useState(false),
    [position, setPosition] = useState(0),
    [duration, setDuration] = useState(0),
    [rate, setRate] = useState(1),
    [loop, setLoop] = useState(false),
    [autoScroll, setAutoScroll] = useState(true),
    [pending, setPending] = useState("");
  const audio = useRef<HTMLAudioElement>(null),
    chapterRef = useRef<Chapter | null>(null),
    bookRef = useRef<Book | null>(null),
    progressRef = useRef<Progress>({
      chapter_id: "",
      segment_id: "",
      clip_id: "",
      seconds: 0,
      revision: 0,
    }),
    progressQueue = useRef<Progress | null>(null),
    saving = useRef(false),
    saveFlight = useRef<Promise<void> | null>(null),
    saveFailed = useRef(false),
    navigation = useRef(0),
    restoreTime = useRef(0),
    autoPlay = useRef(false),
    polling = useRef(false),
    lastJobs = useRef(""),
    refreshGeneration = useRef(0),
    pendingJob = useRef(""),
    pendingRequest = useRef(false),
    prefetched = useRef(new Set<string>()),
    restoring = useRef(false),
    activeAudio = useRef<Clip | null>(null),
    lastSave = useRef(0);
  const clips = (chapter?.segments || []).flatMap((s) => s.clips),
    selectedClip = clips.find((c) => c.id === current),
    selectedSegment = chapter?.segments.find(
      (s) => s.id === selectedClip?.segment_id,
    );
  chapterRef.current = chapter;
  bookRef.current = book;
  const fail = (e: unknown) =>
    setError(e instanceof Error ? e.message : String(e));
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const loadBooks = useCallback(
    async () => setBooks(await api<Book[]>("/books")),
    [],
  );
  useEffect(() => {
    void loadBooks().catch(fail);
    void api<Config>("/config").then(setConfig).catch(fail);
  }, [loadBooks]);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("reader.theme", theme);
  }, [theme]);
  useEffect(() => {
    localStorage.setItem("reader.mode", mode);
    localStorage.setItem("reader.font", String(font));
  }, [mode, font]);
  const refresh = useCallback(async () => {
    const c = chapterRef.current;
    if (!c) return;
    const generation = ++refreshGeneration.current;
    const [updated, list] = await Promise.all([
      api<Chapter>("/chapters/" + c.id),
      api<Chapter[]>("/books/" + c.book_id + "/chapters"),
    ]);
    if (
      chapterRef.current?.id === c.id &&
      generation === refreshGeneration.current
    ) {
      setChapter(updated);
      setChapters(list);
    }
  }, []);
  const flushProgress = (): Promise<void> => {
    if (saveFlight.current) return saveFlight.current;
    if (saveFailed.current) return Promise.resolve();
    const work = async () => {
      saving.current = true;
      try {
        while (progressQueue.current) {
          const p = progressQueue.current;
          progressQueue.current = null;
          const owner = bookRef.current?.id;
          if (!owner) break;
          try {
            const saved = await api<Progress>(
              "/books/" + owner + "/progress",
              "PUT",
              { ...p, revision: progressRef.current.revision },
            );
            if (bookRef.current?.id === owner) progressRef.current = saved;
          } catch (e) {
            progressQueue.current = progressQueue.current || p;
            saveFailed.current = true;
            if (e instanceof ApiError && e.status === 409)
              setError(
                "阅读位置已在另一页面更新。请刷新本页后继续，避免覆盖其他阅读记录。",
              );
            else fail(e);
            break;
          }
        }
      } finally {
        saving.current = false;
      }
    };
    saveFlight.current = work().finally(() => {
      saveFlight.current = null;
    });
    return saveFlight.current;
  };
  const saveProgress = (seg: string, clip = "", seconds = 0) => {
    const c = chapterRef.current;
    if (!c || !seg || !c.segments.some((s) => s.id === seg)) return;
    progressQueue.current = {
      chapter_id: c.id,
      segment_id: seg,
      clip_id: clip,
      seconds,
      revision: progressRef.current.revision,
    };
    void flushProgress();
  };
  useEffect(() => {
    const unload = (event: BeforeUnloadEvent) => {
      if (
        progressQueue.current ||
        saving.current ||
        edit ||
        modal === "import" ||
        modal === "source"
      ) {
        event.preventDefault();
      }
    };
    window.addEventListener("beforeunload", unload);
    return () => window.removeEventListener("beforeunload", unload);
  }, [edit, modal]);
  useEffect(() => {
    if (!chapter) return;
    lastJobs.current = "";
    const events = new EventSource("/api/events?chapter=" + chapter.id);
    events.onmessage = (event) => {
      const list: Job[] = JSON.parse(event.data);
      setJobs(list);
      const signature = JSON.stringify(
        list.map((j) => [j.id, j.status, j.done]),
      );
      if (signature !== lastJobs.current) {
        lastJobs.current = signature;
        void refresh().catch(fail);
      }
    };
    events.onerror = () => {
      /* EventSource reconnects and sends a fresh database snapshot. */
    };
    const fallback = window.setInterval(() => {
      if (events.readyState === EventSource.OPEN || polling.current) return;
      polling.current = true;
      api<Job[]>("/jobs?chapter=" + chapter.id)
        .then(setJobs)
        .then(refresh)
        .catch(fail)
        .finally(() => {
          polling.current = false;
        });
    }, 5000);
    return () => {
      events.close();
      clearInterval(fallback);
    };
  }, [chapter?.id, refresh]);
  const openChapter = async (identity: string, progress?: Progress) => {
    const previous = activeAudio.current;
    if (previous && audio.current && chapterRef.current)
      saveProgress(previous.segment_id, previous.id, audio.current.currentTime);
    activeAudio.current = null;
    audio.current?.pause();
    if (progressQueue.current || saving.current) {
      await flushProgress();
      if (progressQueue.current || saving.current)
        throw new Error("阅读位置尚未保存，请稍后再切换");
    }
    const token = ++navigation.current;
    audio.current?.pause();
    setPending("");
    setCurrent("");
    setPlaying(false);
    const c = await api<Chapter>("/chapters/" + identity);
    if (token !== navigation.current) return;
    setChapter(c);
    chapterRef.current = c;
    setSidebar(false);
    setJobs([]);
    localStorage.setItem("reader.lastBook", c.book_id);
    if (progress?.chapter_id === identity) {
      restoring.current = true;
      const clip = c.segments
        .flatMap((s) => s.clips)
        .find((a) => a.id === progress.clip_id);
      if (clip?.asset_id) {
        restoreTime.current = progress.seconds;
        autoPlay.current = false;
        setCurrent(clip.id);
      }
      setTimeout(() => {
        document
          .getElementById("segment-" + progress.segment_id)
          ?.scrollIntoView({ block: "center" });
        setTimeout(() => {
          restoring.current = false;
        }, 400);
      }, 120);
    } else {
      window.scrollTo({ top: 0 });
      if (c.segments[0]) {
        progressQueue.current = {
          chapter_id: c.id,
          segment_id: c.segments[0].id,
          clip_id: "",
          seconds: 0,
          revision: progressRef.current.revision,
        };
        void flushProgress();
      }
    }
  };
  const openBook = async (b: Book) => {
    const [list, p] = await Promise.all([
      api<Chapter[]>("/books/" + b.id + "/chapters"),
      api<Progress>("/books/" + b.id + "/progress"),
    ]);
    setBook(b);
    bookRef.current = b;
    setChapters(list);
    setChapter(null);
    chapterRef.current = null;
    progressRef.current = p;
    progressQueue.current = null;
    saveFailed.current = false;
    if (list.length)
      await openChapter(
        list.find((c) => c.id === p.chapter_id)?.id || list[0].id,
        p,
      );
  };
  const goHome = () => {
    void run(async () => {
      const previous = activeAudio.current;
      if (previous && audio.current)
        saveProgress(
          previous.segment_id,
          previous.id,
          audio.current.currentTime,
        );
      activeAudio.current = null;
      audio.current?.pause();
      await flushProgress();
      if (saving.current || progressQueue.current) {
        setError("阅读位置尚未保存，请重试保存后返回");
        return;
      }
      navigation.current++;
      audio.current?.pause();
      setBook(null);
      setChapter(null);
      setCurrent("");
      setPending("");
      setPlaying(false);
      void loadBooks().catch(fail);
    });
  };
  const startJob = async (
    kind: "translation" | "audio",
    segments: string[] = [],
    force = false,
  ) => {
    const c = chapterRef.current;
    if (!c) return;
    const j = await api<Job>(`/chapters/${c.id}/${kind}-jobs`, "POST", {
      revision: c.revision,
      segments,
      force,
    });
    setJobs((old) => [j, ...old.filter((x) => x.id !== j.id)]);
    return j;
  };
  const chooseClip = async (clip: Clip) => {
    if (!clip.asset_id) {
      audio.current?.pause();
      setPlaying(false);
      setPending(clip.id);
      setNotice("正在准备这一段的语音…");
      pendingRequest.current = true;
      try {
        const j = await startJob("audio", [clip.segment_id]);
        pendingJob.current = j?.id || "";
      } catch (e) {
        setPending("");
        setNotice("");
        throw e;
      } finally {
        pendingRequest.current = false;
      }
      return;
    }
    setPending("");
    setNotice("");
    restoreTime.current = 0;
    autoPlay.current = true;
    if (current === clip.id && audio.current) {
      audio.current.currentTime = 0;
      await audio.current.play();
    } else setCurrent(clip.id);
    saveProgress(clip.segment_id, clip.id, 0);
  };
  useEffect(() => {
    if (!selectedClip?.asset_id) return;
    const a = audio.current;
    if (!a) return;
    activeAudio.current = selectedClip;
    a.src = audioURL(selectedClip.asset_id);
    a.load();
    setPosition(0);
    setDuration(selectedClip.duration);
    if (autoScroll)
      document
        .getElementById("segment-" + selectedClip.segment_id)
        ?.scrollIntoView({ behavior: "smooth", block: "center" });
  }, [current, selectedClip?.asset_id]);
  useEffect(() => {
    if (audio.current) audio.current.playbackRate = rate;
  }, [rate]);
  useEffect(() => {
    if (!pending) return;
    const clip = clips.find((c) => c.id === pending);
    if (clip?.asset_id) {
      void chooseClip(clip).catch(fail);
    } else if (!pendingRequest.current) {
      const j = jobs.find((j) => j.id === pendingJob.current);
      if (j && !active(j)) {
        if (j.status === "succeeded" && clip) {
          pendingRequest.current = true;
          void refresh()
            .then(() => startJob("audio", [clip.segment_id]))
            .then((next) => {
              pendingJob.current = next?.id || "";
            })
            .catch((e) => {
              setPending("");
              fail(e);
            })
            .finally(() => {
              pendingRequest.current = false;
            });
        } else {
          setPending("");
          setNotice("");
          setError("语音尚未准备完成，请在处理记录中查看原因并重试。");
        }
      }
    }
  }, [chapter, jobs, pending]);
  useEffect(() => {
    if (
      !playing ||
      !current ||
      jobs.some((j) => j.kind === "audio" && active(j))
    )
      return;
    const upcoming = clips.slice(
      clips.findIndex((c) => c.id === current) + 1,
      clips.findIndex((c) => c.id === current) + 3,
    );
    const missing = upcoming.filter(
      (c) => !c.asset_id && !prefetched.current.has(c.id),
    );
    missing.forEach((c) => prefetched.current.add(c.id));
    if (missing.length)
      void startJob("audio", [
        ...new Set(missing.map((c) => c.segment_id)),
      ]).catch(fail);
    upcoming
      .filter((c) => c.asset_id)
      .forEach((c) => {
        if (!prefetched.current.has(c.asset_id)) {
          prefetched.current.add(c.asset_id);
          void fetch(audioURL(c.asset_id))
            .then((r) => r.arrayBuffer())
            .catch(() => {});
        }
      });
  }, [current, playing, chapter, jobs]);
  const move = async (delta: number) => {
    const index = clips.findIndex((c) => c.id === current);
    const next = clips[index + delta];
    if (next) await chooseClip(next);
    else {
      setPlaying(false);
      setNotice("本章已播放完毕");
    }
  };
  const togglePlay = async () => {
    const a = audio.current;
    if (!a) return;
    if (playing) a.pause();
    else if (selectedClip?.asset_id) await a.play();
    else if (clips[0]) await chooseClip(clips[0]);
  };
  const loaded = () => {
    const a = audio.current!;
    a.playbackRate = rate;
    a.currentTime = Math.min(
      restoreTime.current,
      Math.max(0, a.duration - 0.1),
    );
    restoreTime.current = 0;
    setDuration(a.duration);
    if (autoPlay.current) {
      autoPlay.current = false;
      void a.play().catch(() => setNotice("语音已就绪，点击播放开始收听"));
    }
  };
  const timeUpdate = () => {
    const a = audio.current!;
    setPosition(a.currentTime);
    if (Date.now() - lastSave.current > 5000 && selectedClip) {
      lastSave.current = Date.now();
      saveProgress(selectedClip.segment_id, selectedClip.id, a.currentTime);
    }
  };
  useEffect(() => {
    if (!chapter) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (playing || pending || restoring.current || current) return;
        const entry = entries
          .filter((e) => e.isIntersecting)
          .sort(
            (a, b) => a.boundingClientRect.top - b.boundingClientRect.top,
          )[0];
        if (entry) {
          const seg = entry.target.getAttribute("data-segment");
          if (seg) {
            const p = progressQueue.current || progressRef.current;
            if (p.segment_id !== seg) saveProgress(seg);
          }
        }
      },
      { rootMargin: "-15% 0px -65% 0px" },
    );
    document
      .querySelectorAll("[data-segment]")
      .forEach((e) => observer.observe(e));
    return () => observer.disconnect();
  }, [chapter?.id, chapter?.segments.length, playing, pending]);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    await run(async () => {
      if (modal === "book") {
        const b = await api<Book>("/books", "POST", { title, author });
        await loadBooks();
        await openBook(b);
        setModal(null);
      } else if (modal === "import" && book) {
        const v = await api<{ id: string }>(
          "/books/" + book.id + "/chapters",
          "POST",
          { title, raw },
        );
        setModal(null);
        setChapters(await api<Chapter[]>("/books/" + book.id + "/chapters"));
        await openChapter(v.id);
        setNotice("章节已保存，可点击“生成翻译”开始。");
      } else if (modal === "source" && chapter) {
        activeAudio.current = null;
        audio.current?.pause();
        await flushProgress();
        if (progressQueue.current) throw new Error("请先处理阅读位置保存错误");
        await api("/chapters/" + chapter.id + "/source", "PATCH", {
          title,
          raw,
          revision: chapter.revision,
        });
        audio.current?.pause();
        setCurrent("");
        setPending("");
        progressQueue.current = null;
        progressRef.current = await api<Progress>(
          "/books/" + chapter.book_id + "/progress",
        );
        await refresh();
        setModal(null);
      }
    });
  };
  const showImport = () => {
    setTitle("");
    setRaw("");
    setModal("import");
  };
  const showGlossary = () =>
    void run(async () => {
      if (book) {
        setGlossary(await api<Glossary>("/books/" + book.id + "/glossary"));
        setModal("glossary");
      }
    });
  const showDetail = (j: Job) =>
    void run(async () => {
      const [value, usage] = await Promise.all([
        api<Job>("/jobs/" + j.id),
        api<unknown[]>("/jobs/" + j.id + "/attempts"),
      ]);
      setDetail(value);
      setAttempts(usage);
    });
  const ready = chapter?.segments.filter((s) => s.translation).length || 0;
  return (
    <div className="app">
      <header className="topbar">
        <button className="brand" onClick={goHome}>
          <span className="brand-mark">栞</span>
          <span>
            SHIORI<small>在文字之间，听见故事</small>
          </span>
        </button>
        <nav>
          {book && (
            <button onClick={goHome}>
              <Library size={17} />
              书架
            </button>
          )}
          <button
            className="icon"
            aria-label="切换主题"
            onClick={() => setTheme(theme === "light" ? "dark" : "light")}
          >
            {theme === "light" ? <Moon size={19} /> : <Sun size={19} />}
          </button>
          <button
            className="icon"
            aria-label="设置"
            onClick={() => setModal("settings")}
          >
            <Settings size={19} />
          </button>
        </nav>
      </header>
      {error && (
        <div role="alert" className="banner error">
          <span>{error}</span>
          {saveFailed.current && (
            <button
              onClick={() => {
                saveFailed.current = false;
                void flushProgress();
              }}
            >
              重试保存
            </button>
          )}
          <button aria-label="关闭错误" onClick={() => setError("")}>
            <X size={16} />
          </button>
        </div>
      )}
      {notice && (
        <div role="status" className="banner notice">
          <span>{notice}</span>
          <button aria-label="关闭提示" onClick={() => setNotice("")}>
            <X size={16} />
          </button>
        </div>
      )}
      {!book ? (
        <main className="shelf">
          <div className="hero">
            <div>
              <p className="eyebrow">YOUR PRIVATE READING ROOM</p>
              <h1>
                让每个故事，
                <br />
                <em>以熟悉的语言抵达。</em>
              </h1>
              <p className="intro">
                收藏日文原作，慢慢读，也慢慢听。
                <br />
                让对照译文与自然朗读，陪你走进故事。
              </p>
            </div>
            <div className="hero-art" aria-hidden="true">
              <div className="paper paper-back" />
              <div className="paper">
                <span>物語の、その先へ。</span>
                <div className="paper-line" />
                <small>読む · 聴く · 感じる</small>
              </div>
              <span className="stamp">栞</span>
            </div>
          </div>
          <div className="section-heading">
            <div>
              <p className="eyebrow">LIBRARY</p>
              <h2>
                我的书架 <span className="count">{books.length}</span>
              </h2>
            </div>
            <Button
              onClick={() => {
                setTitle("");
                setAuthor("");
                setModal("book");
              }}
            >
              <Plus size={17} />
              创建小说
            </Button>
          </div>
          {books.length === 0 ? (
            <div className="empty">
              <BookOpen size={36} />
              <h3>故事，从一段文字开始</h3>
              <p>创建一本小说，粘贴日文原文，即可开始对照阅读。</p>
              <button
                className="text-button"
                onClick={() => {
                  setTitle("");
                  setAuthor("");
                  setModal("book");
                }}
              >
                创建第一本小说 <ChevronRight size={16} />
              </button>
            </div>
          ) : (
            <div className="book-grid">
              {books.map((b, n) => (
                <button
                  className="book-card"
                  key={b.id}
                  onClick={() => void run(() => openBook(b))}
                >
                  <div className={"cover cover-" + (n % 4)}>
                    <small>日文原作 / 私人藏书</small>
                    <h3>{b.title}</h3>
                    <span>{b.author || "未署名"}</span>
                    <BookOpen className="cover-icon" size={80} />
                  </div>
                  <div className="book-meta">
                    <h3>{b.title}</h3>
                    <p>
                      {b.chapter_count} 个章节 <span>开始阅读 →</span>
                    </p>
                  </div>
                </button>
              ))}
            </div>
          )}
          <footer className="shelf-footer">
            一页原文，一段新世界。<span>文本与阅读记录保存在本机</span>
          </footer>
        </main>
      ) : (
        <div className="workspace">
          <aside className={sidebar ? "sidebar visible" : "sidebar"}>
            <button
              className="mobile-toggle icon sidebar-close"
              aria-label="关闭目录"
              onClick={() => setSidebar(false)}
            >
              <X size={18} />
            </button>
            <button className="back-link" onClick={goHome}>
              <ArrowLeft size={15} />
              返回书架
            </button>
            <p className="eyebrow">CONTENTS</p>
            <h2>{book.title}</h2>
            <p className="muted">
              {book.author || "日文小说"} · {chapters.length} 章
            </p>
            <Button secondary onClick={showImport}>
              <Plus size={16} />
              添加章节
            </Button>
            <div className="chapter-list">
              {chapters.map((c, n) => (
                <button
                  key={c.id}
                  className={
                    chapter?.id === c.id
                      ? "chapter-link selected"
                      : "chapter-link"
                  }
                  onClick={() => void run(() => openChapter(c.id))}
                >
                  <span className="chapter-no">
                    {String(n + 1).padStart(2, "0")}
                  </span>
                  <span>
                    {c.title}
                    <small>
                      {c.translated > 0
                        ? `${c.translated}/${c.total} 段已翻译`
                        : "等待翻译"}
                    </small>
                  </span>
                  {c.total > 0 && c.total === c.translated && (
                    <Check size={15} />
                  )}
                </button>
              ))}
            </div>
            <button className="glossary-link" onClick={showGlossary}>
              <Languages size={17} />
              人名与术语表
            </button>
            <div className="sidebar-note">
              <Bookmark size={17} />
              <p>
                阅读位置自动保存
                <br />
                <span>下次从这里继续</span>
              </p>
            </div>
          </aside>
          <main className="reader">
            <div className="reader-toolbar">
              <button
                className="mobile-toggle icon"
                aria-label="章节目录"
                onClick={() => setSidebar(!sidebar)}
              >
                <List size={19} />
              </button>
              <div className="mode-tabs" aria-label="阅读模式">
                {[
                  ["parallel", "对照"],
                  ["ja", "日文"],
                  ["zh", "中文"],
                ].map(([v, label]) => (
                  <button
                    key={v}
                    className={mode === v ? "selected" : ""}
                    onClick={() => setMode(v)}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <div className="font-tools">
                <button
                  aria-label="缩小字号"
                  onClick={() => setFont(Math.max(15, font - 1))}
                >
                  A−
                </button>
                <button
                  aria-label="放大字号"
                  onClick={() => setFont(Math.min(28, font + 1))}
                >
                  A＋
                </button>
              </div>
            </div>
            {chapter ? (
              <>
                <div className="chapter-heading">
                  <p className="eyebrow">
                    CHAPTER {String(chapter.position + 1).padStart(2, "0")}
                  </p>
                  <h1>{chapter.title}</h1>
                  <div className="chapter-description">
                    <span>
                      {Array.from(chapter.raw).length.toLocaleString()} 字
                    </span>
                    <span>
                      {ready}/{chapter.segments.length} 段已翻译
                    </span>
                    <button
                      onClick={() => {
                        setTitle(chapter.title);
                        setRaw(chapter.raw);
                        setModal("source");
                      }}
                    >
                      <Pencil size={13} />
                      编辑原文
                    </button>
                  </div>
                  <div className="chapter-actions">
                    <Button
                      disabled={
                        busy ||
                        jobs.some((j) => j.kind === "translation" && active(j))
                      }
                      onClick={() =>
                        void run(async () => {
                          await startJob("translation");
                        })
                      }
                    >
                      <Languages size={16} />
                      生成翻译
                    </Button>
                    <Button
                      secondary
                      disabled={
                        busy ||
                        jobs.some((j) => j.kind === "audio" && active(j))
                      }
                      onClick={() =>
                        void run(async () => {
                          await startJob("audio");
                        })
                      }
                    >
                      <Headphones size={16} />
                      准备整章语音
                    </Button>
                  </div>
                </div>
                {jobs.length > 0 && (
                  <section className="jobs" aria-label="处理记录">
                    {jobs.slice(0, 3).map((j) => (
                      <div className={"job " + j.status} key={j.id}>
                        <button
                          className="job-info"
                          onClick={() => showDetail(j)}
                        >
                          {active(j) ? (
                            <LoaderCircle size={15} className="spin" />
                          ) : j.status === "succeeded" ? (
                            <Check size={15} />
                          ) : (
                            <span className="status-dot" />
                          )}
                          <span>
                            {j.kind === "translation" ? "翻译" : "语音"} ·{" "}
                            {states[j.status]}
                          </span>
                          <small>
                            {j.done}/{j.total}
                          </small>
                        </button>
                        {active(j) ? (
                          <button
                            onClick={() =>
                              void run(async () => {
                                await api("/jobs/" + j.id + "/cancel", "POST");
                              })
                            }
                          >
                            停止
                          </button>
                        ) : (
                          j.status !== "succeeded" && (
                            <button
                              onClick={() =>
                                void run(async () => {
                                  await api("/jobs/" + j.id + "/retry", "POST");
                                  await refresh();
                                })
                              }
                            >
                              继续 / 重试
                            </button>
                          )
                        )}
                      </div>
                    ))}
                  </section>
                )}
                <div
                  className={"reading-text " + mode}
                  style={
                    { "--reading-size": font + "px" } as React.CSSProperties
                  }
                >
                  {mode === "parallel" && (
                    <div className="column-labels">
                      <span>日本語 · 原文</span>
                      <span>中文 · 译文</span>
                    </div>
                  )}
                  {chapter.segments.map((seg, n) => (
                    <article
                      id={"segment-" + seg.id}
                      data-segment={seg.id}
                      key={seg.id}
                      className={
                        "segment " +
                        (selectedSegment?.id === seg.id ? "is-current" : "")
                      }
                    >
                      <div className="segment-number">
                        {String(n + 1).padStart(2, "0")}
                      </div>
                      {mode !== "zh" && (
                        <div className="japanese">
                          <p lang="ja">{seg.text}</p>
                          <div className="segment-tools">
                            <button
                              aria-label={"朗读第" + (n + 1) + "段"}
                              disabled={busy || pending === seg.clips[0]?.id}
                              onClick={() =>
                                void run(async () => {
                                  if (seg.clips[0])
                                    await chooseClip(seg.clips[0]);
                                })
                              }
                            >
                              {pending === seg.clips[0]?.id ? (
                                <LoaderCircle size={14} className="spin" />
                              ) : (
                                <Volume2 size={14} />
                              )}
                              朗读
                            </button>
                            <button
                              onClick={() => {
                                setEdit({ segment: seg, kind: "speech" });
                                setDraft(seg.speech_text);
                              }}
                            >
                              调整读音
                            </button>
                            {seg.clips.every((c) => c.asset_id) && (
                              <span className="audio-ready">
                                <Check size={12} />
                                语音就绪
                              </span>
                            )}
                          </div>
                        </div>
                      )}
                      {mode !== "ja" && (
                        <div className="translation">
                          <p>
                            {seg.translation || (
                              <span className="untranslated">
                                译文将在生成后显示在这里
                              </span>
                            )}
                          </p>
                          <div className="segment-tools">
                            <button
                              onClick={() => {
                                setEdit({ segment: seg, kind: "translation" });
                                setDraft(seg.translation);
                              }}
                            >
                              <Pencil size={12} />
                              {seg.translation ? "校对译文" : "填写译文"}
                            </button>
                            {seg.manual && <span>已人工校对</span>}
                            {!seg.manual && seg.translation && (
                              <button
                                disabled={busy}
                                onClick={() =>
                                  void run(async () => {
                                    await startJob(
                                      "translation",
                                      [seg.id],
                                      true,
                                    );
                                  })
                                }
                              >
                                重新翻译
                              </button>
                            )}
                          </div>
                        </div>
                      )}
                    </article>
                  ))}
                </div>
                <div className="chapter-end">
                  <span>＊</span>
                  <p>本章终</p>
                  {chapters[chapter.position + 1] && (
                    <Button
                      secondary
                      onClick={() =>
                        void run(() =>
                          openChapter(chapters[chapter.position + 1].id),
                        )
                      }
                    >
                      下一章
                      <ChevronRight size={16} />
                    </Button>
                  )}
                </div>
              </>
            ) : (
              <div className="empty">
                <BookOpen size={40} />
                <h2>书已备好，等故事落笔</h2>
                <p>添加第一章，粘贴你想阅读的日文小说。</p>
                <Button onClick={showImport}>
                  <Plus size={16} />
                  添加章节
                </Button>
              </div>
            )}
          </main>
        </div>
      )}
      {chapter && (
        <div className="player">
          <div className="player-label">
            <span className="sound-icon">
              <Headphones size={20} />
            </span>
            <div>
              <strong>
                {pending
                  ? "正在准备语音…"
                  : selectedSegment
                    ? `第 ${selectedSegment.position + 1} 段 · 日语朗读`
                    : "听一听这个故事"}
              </strong>
              <small>{chapter.title}</small>
            </div>
          </div>
          <div className="player-center">
            <div className="play-controls">
              <button
                className="icon"
                aria-label="上一段"
                disabled={!current || clips[0]?.id === current}
                onClick={() => void run(() => move(-1))}
              >
                <SkipBack size={18} />
              </button>
              <button
                className="play-main"
                aria-label={playing ? "暂停" : "播放"}
                disabled={!!pending}
                onClick={() => void run(togglePlay)}
              >
                {pending ? (
                  <LoaderCircle size={22} className="spin" />
                ) : playing ? (
                  <Pause size={22} />
                ) : (
                  <Play size={22} />
                )}
              </button>
              <button
                className="icon"
                aria-label="下一段"
                disabled={!current || clips.at(-1)?.id === current}
                onClick={() => void run(() => move(1))}
              >
                <SkipForward size={18} />
              </button>
            </div>
            <div className="timeline">
              <span>{formatTime(position)}</span>
              <input
                aria-label="播放进度"
                type="range"
                min="0"
                max={duration || 1}
                step="0.1"
                value={Math.min(position, duration || 1)}
                disabled={!current}
                onChange={(e) => {
                  if (audio.current) {
                    audio.current.currentTime = Number(e.target.value);
                    setPosition(Number(e.target.value));
                  }
                }}
              />
              <span>{formatTime(duration)}</span>
            </div>
          </div>
          <div className="player-options">
            <select
              aria-label="播放速度"
              value={rate}
              onChange={(e) => setRate(Number(e.target.value))}
            >
              {[0.65, 0.8, 1, 1.2, 1.5, 2].map((n) => (
                <option value={n} key={n}>
                  {n}×
                </option>
              ))}
            </select>
            <button
              className={"icon " + (loop ? "enabled" : "")}
              aria-label="循环当前片段"
              aria-pressed={loop}
              onClick={() => setLoop(!loop)}
            >
              <Repeat size={18} />
            </button>
            <label>
              <input
                type="checkbox"
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
              />
              跟随
            </label>
          </div>
        </div>
      )}
      <audio
        ref={audio}
        onLoadedMetadata={loaded}
        onTimeUpdate={timeUpdate}
        onPlay={() => setPlaying(true)}
        onPause={() => {
          setPlaying(false);
          if (activeAudio.current)
            saveProgress(
              activeAudio.current.segment_id,
              activeAudio.current.id,
              audio.current?.currentTime || 0,
            );
        }}
        onEnded={() => {
          if (loop) {
            audio.current!.currentTime = 0;
            void audio.current!.play().catch(fail);
          } else void move(1).catch(fail);
        }}
        onError={() => {
          if (current) {
            setPlaying(false);
            setError("音频播放失败，请重试或重新生成该段语音。");
          }
        }}
      />
      {(modal === "book" || modal === "import" || modal === "source") && (
        <Modal
          title={
            modal === "book"
              ? "创建小说"
              : modal === "source"
                ? "编辑章节原文"
                : "添加新章节"
          }
          onClose={() => {
            if ((raw || title) && !window.confirm("放弃尚未保存的内容？"))
              return;
            setModal(null);
          }}
          wide={modal !== "book"}
        >
          <form onSubmit={submit}>
            <label>
              {modal === "book" ? "小说名称" : "章节标题"}
              <input
                autoFocus
                required
                maxLength={200}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={
                  modal === "book"
                    ? "给故事一个名字"
                    : "例如：第一章 夏の終わり"
                }
              />
            </label>
            {modal === "book" ? (
              <label>
                作者（选填）
                <input
                  value={author}
                  onChange={(e) => setAuthor(e.target.value)}
                />
              </label>
            ) : (
              <>
                <label>
                  日文原文
                  <textarea
                    className="source-input"
                    required
                    value={raw}
                    onChange={(e) => setRaw(e.target.value)}
                    placeholder="在这里粘贴日文小说文本，保留原有段落和换行。"
                  />
                </label>
                <div className="import-meta">
                  {Array.from(raw).length.toLocaleString()} 字 ·{" "}
                  {raw.split("\n").filter((s) => s.trim()).length} 个自然段
                </div>
                <details className="preview">
                  <summary>分段预览</summary>
                  {raw
                    .split("\n")
                    .filter((s) => s.trim())
                    .slice(0, 8)
                    .map((line, i) => (
                      <p key={i}>
                        <small>{i + 1}</small>
                        {line}
                      </p>
                    ))}
                  <small>长段落会在句界附近拆分，原文完整保留。</small>
                </details>
                {modal === "source" && (
                  <p className="muted">
                    修改原文将创建新版本，并重新生成段落对应关系。旧原文和译文保留在历史记录中。
                  </p>
                )}
              </>
            )}
            <div className="modal-footer">
              <button className="button" disabled={busy}>
                {busy ? (
                  <LoaderCircle className="spin" size={16} />
                ) : (
                  <Check size={16} />
                )}
                保存{modal === "book" ? "小说" : "章节"}
              </button>
            </div>
          </form>
        </Modal>
      )}
      {edit && (
        <Modal
          title={
            edit.kind === "translation" ? "校对中文译文" : "调整日语朗读文本"
          }
          onClose={() => {
            if (
              draft !==
                (edit.kind === "translation"
                  ? edit.segment.translation
                  : edit.segment.speech_text) &&
              !window.confirm("放弃尚未保存的修改？")
            )
              return;
            setEdit(null);
          }}
          wide
        >
          <p className="source-preview" lang="ja">
            {edit.segment.text}
          </p>
          <label>
            {edit.kind === "translation" ? "中文译文" : "朗读文本"}
            <textarea
              aria-label={edit.kind === "translation" ? "中文译文" : "朗读文本"}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              rows={7}
            />
          </label>
          <p className="muted">
            {edit.kind === "translation"
              ? "人工译文会被保留，普通生成和重试不会覆盖它。"
              : "可用假名纠正人名读音，不会改动日文原文。保存后需重新准备此段语音。"}
          </p>
          <div className="modal-footer">
            <Button
              disabled={busy || !draft.trim()}
              onClick={() =>
                void run(async () => {
                  if (edit.kind === "speech") {
                    activeAudio.current = null;
                    audio.current?.pause();
                    await flushProgress();
                    if (progressQueue.current)
                      throw new Error("请先处理阅读位置保存错误");
                  }
                  await api(
                    "/segments/" +
                      edit.segment.id +
                      "/" +
                      (edit.kind === "translation"
                        ? "translation"
                        : "speech-text"),
                    "PATCH",
                    {
                      text: draft,
                      revision:
                        edit.kind === "translation"
                          ? edit.segment.translation_revision
                          : edit.segment.speech_revision,
                    },
                  );
                  if (edit.kind === "speech") {
                    audio.current?.pause();
                    setCurrent("");
                    setPending("");
                    progressRef.current = await api<Progress>(
                      "/books/" + book!.id + "/progress",
                    );
                    progressQueue.current = null;
                  }
                  setEdit(null);
                  await refresh();
                })
              }
            >
              保存修改
            </Button>
            {edit.kind === "speech" && (
              <Button
                secondary
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await startJob("audio", [edit.segment.id], true);
                    setEdit(null);
                  })
                }
              >
                重新生成现有朗读
              </Button>
            )}
          </div>
        </Modal>
      )}
      {modal === "glossary" && (
        <Modal
          title="人名与术语表"
          onClose={() => {
            if (!window.confirm("关闭术语表？未保存的修改将丢失。")) return;
            setModal(null);
          }}
          wide
        >
          <p className="muted">
            固定小说中的人名、地名与称谓译法。修改后新任务使用新术语；已有译文保持不变。读音仅作为翻译参考，语音读法请在段落中调整。
          </p>
          <div className="term-head">
            <span>日文</span>
            <span>中文译名</span>
            <span>读音（选填）</span>
          </div>
          {glossary.terms.map((term, i) => (
            <div className="term-row" key={i}>
              {(["source", "target", "reading"] as const).map((field) => (
                <input
                  key={field}
                  aria-label={`${i + 1}-${field}`}
                  value={term[field]}
                  onChange={(e) =>
                    setGlossary((g) => ({
                      ...g,
                      terms: g.terms.map((t, n) =>
                        n === i ? { ...t, [field]: e.target.value } : t,
                      ),
                    }))
                  }
                />
              ))}
              <button
                className="icon"
                aria-label={"删除术语" + (i + 1)}
                onClick={() =>
                  setGlossary((g) => ({
                    ...g,
                    terms: g.terms.filter((_, n) => n !== i),
                  }))
                }
              >
                <Trash2 size={16} />
              </button>
            </div>
          ))}
          <button
            className="text-button"
            onClick={() =>
              setGlossary((g) => ({
                ...g,
                terms: [...g.terms, { source: "", target: "", reading: "" }],
              }))
            }
          >
            <Plus size={16} />
            添加术语
          </button>
          <div className="modal-footer">
            <Button
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  setGlossary(
                    await api<Glossary>(
                      "/books/" + book!.id + "/glossary",
                      "PUT",
                      glossary,
                    ),
                  );
                  setModal(null);
                  setNotice("术语表已保存");
                })
              }
            >
              保存术语表
            </Button>
          </div>
        </Modal>
      )}
      {modal === "settings" && (
        <Modal title="阅读与服务设置" onClose={() => setModal(null)}>
          <p className="eyebrow">MODEL CONNECTIONS</p>
          <div className="setting-row">
            <span>
              对照翻译<small>{config?.model || "尚未选择模型"}</small>
            </span>
            <span
              className={config?.translation_ready ? "badge" : "badge muted"}
            >
              {config?.translation_ready ? "已配置" : "待配置"}
            </span>
          </div>
          <div className="setting-row">
            <span>
              日语朗读
              <small>
                {config?.tts_provider} · {config?.tts_model}
                <br />
                {config?.voice}
              </small>
            </span>
            <span className={config?.tts_ready ? "badge" : "badge muted"}>
              {config?.tts_ready ? "已配置" : "待配置"}
            </span>
          </div>
          <div className="setting-row">
            <span>
              音频存储
              <small>
                {config?.flac_enabled
                  ? "WAV → 后台 FLAC 无损压缩"
                  : "WAV（FFmpeg 不可用）"}
              </small>
            </span>
            <Headphones size={20} />
          </div>
          <p className="muted">
            服务地址、密钥、模型和声音在项目 .env
            中设置，重启后生效。已配置不代表连接已验证。生成内容会发送给你配置的模型服务。
          </p>
          <Button
            secondary
            disabled={busy}
            onClick={() =>
              void run(async () => {
                const result = await api<{ removed: number }>(
                  "/cache/cleanup",
                  "POST",
                );
                setNotice(`已清理 ${result.removed} 个不再使用的音频资源`);
              })
            }
          >
            <Trash2 size={16} />
            清理未使用音频
          </Button>
        </Modal>
      )}
      {detail && (
        <Modal title="处理记录" onClose={() => setDetail(null)} wide>
          <p>
            {detail.kind === "translation" ? "对照翻译" : "语音生成"} ·{" "}
            {states[detail.status]} · {detail.done}/{detail.total}
          </p>
          <p className="muted">{detail.message}</p>
          {detail.items?.map((item) => (
            <div className="attempt" key={item.id}>
              <strong>
                片段 {item.position + 1} · {states[item.status]}
              </strong>
              {item.message && <p>{item.message}</p>}
            </div>
          ))}
          <details>
            <summary>调用耗时与返回用量</summary>
            <p className="muted">
              用量为空表示服务未返回数据，不代表没有计费。
            </p>
            <pre>{JSON.stringify(attempts, null, 2)}</pre>
          </details>
        </Modal>
      )}
    </div>
  );
}
