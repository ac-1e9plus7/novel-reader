package app

import (
	"database/sql"
	_ "embed"
	"fmt"
	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed schema.sql
var schema string

type Store struct {
	DB    *sql.DB
	Root  string
	mu    sync.Mutex
	lease *flock.Flock
}

func OpenStore(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "audio"), 0700); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(abs, ".instance.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("此数据目录已有服务运行")
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, "reader.sqlite3"))
	if err != nil {
		_ = lock.Unlock()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var hasMigrations int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&hasMigrations); err != nil {
		db.Close()
		lock.Unlock()
		return nil, err
	}
	if hasMigrations > 0 {
		var version int
		if err = db.QueryRow("SELECT coalesce(max(version),0) FROM schema_migrations").Scan(&version); err != nil || version != 1 {
			db.Close()
			lock.Unlock()
			return nil, fmt.Errorf("数据库版本不受支持，请使用匹配的应用版本")
		}
	}
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		lock.Unlock()
		return nil, err
	}
	s := &Store{DB: db, Root: abs, lease: lock}
	_, err = db.Exec("UPDATE jobs SET status='interrupted',message='服务已重启，可继续未完成部分' WHERE status IN ('queued','running'); UPDATE job_items SET status='queued' WHERE status='running'")
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() { s.DB.Close(); s.lease.Unlock() }
func (s *Store) Books() ([]Book, error) {
	rows, e := s.DB.Query(`SELECT b.id,b.title,b.author,b.created,b.glossary_revision,count(c.id) FROM books b LEFT JOIN chapters c ON c.book_id=b.id GROUP BY b.id ORDER BY b.created DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Book{}
	for rows.Next() {
		var b Book
		if e = rows.Scan(&b.ID, &b.Title, &b.Author, &b.Created, &b.GlossaryRevision, &b.ChapterCount); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) CreateBook(title, author string) (Book, error) {
	b := Book{ID: id(), Title: strings.TrimSpace(title), Author: strings.TrimSpace(author), Created: now(), GlossaryRevision: 1}
	if b.Title == "" || len([]rune(b.Title)) > 200 {
		return b, fmt.Errorf("书名须为 1–200 字")
	}
	_, e := s.DB.Exec("INSERT INTO books(id,title,author,created) VALUES(?,?,?,?)", b.ID, b.Title, b.Author, b.Created)
	return b, e
}
func insertSegments(tx *sql.Tx, chapter string, raw string) error {
	for _, seg := range segments(raw) {
		if _, e := tx.Exec("INSERT INTO segments(id,chapter_id,position,paragraph,start,end,text,speech_text) VALUES(?,?,?,?,?,?,?,?)", seg.ID, chapter, seg.Position, seg.Paragraph, seg.Start, seg.End, seg.Text, seg.SpeechText); e != nil {
			return e
		}
		if e := insertClips(tx, seg.ID, seg.SpeechText); e != nil {
			return e
		}
	}
	return nil
}
func insertClips(tx *sql.Tx, segment, text string) error {
	for n, part := range Split(text, 280) {
		if _, e := tx.Exec("INSERT INTO audio_clips(id,segment_id,position,text) VALUES(?,?,?,?)", id(), segment, n, part); e != nil {
			return e
		}
	}
	return nil
}
func validateSource(title, raw string) error {
	if strings.TrimSpace(title) == "" || len([]rune(title)) > 200 {
		return fmt.Errorf("章节标题须为 1–200 字")
	}
	if strings.TrimSpace(raw) == "" || len([]rune(raw)) > 200000 {
		return fmt.Errorf("原文须为 1–200000 字")
	}
	return nil
}
func (s *Store) CreateChapter(book, title, raw string) (string, error) {
	if e := validateSource(title, raw); e != nil {
		return "", e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRow("SELECT count(*) FROM books WHERE id=?", book).Scan(&exists); e != nil {
		return "", e
	}
	if exists == 0 {
		return "", ErrNotFound
	}
	identity := id()
	_, e = tx.Exec("INSERT INTO chapters VALUES(?,?,?,(SELECT coalesce(max(position),-1)+1 FROM chapters WHERE book_id=?),1,?)", identity, book, title, book, raw)
	if e != nil {
		return "", e
	}
	if _, e = tx.Exec("INSERT INTO chapter_revisions VALUES(?,1,?,?)", identity, raw, now()); e != nil {
		return "", e
	}
	if e = insertSegments(tx, identity, raw); e != nil {
		return "", e
	}
	return identity, tx.Commit()
}
func (s *Store) Chapters(book string) ([]Chapter, error) {
	rows, e := s.DB.Query(`SELECT c.id,c.book_id,c.title,c.position,c.revision,(SELECT count(*) FROM segments WHERE chapter_id=c.id),(SELECT count(*) FROM translations t JOIN segments s ON s.id=t.segment_id WHERE s.chapter_id=c.id),(SELECT count(*) FROM audio_clips a JOIN segments s ON s.id=a.segment_id WHERE s.chapter_id=c.id AND a.asset_id IS NOT NULL) FROM chapters c WHERE c.book_id=? ORDER BY c.position`, book)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Chapter{}
	for rows.Next() {
		var c Chapter
		if e = rows.Scan(&c.ID, &c.BookID, &c.Title, &c.Position, &c.Revision, &c.Total, &c.Translated, &c.AudioReady); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) Chapter(identity string) (Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chapter(identity)
}
func (s *Store) chapter(identity string) (Chapter, error) {
	var c Chapter
	e := s.DB.QueryRow("SELECT id,book_id,title,position,revision,raw FROM chapters WHERE id=?", identity).Scan(&c.ID, &c.BookID, &c.Title, &c.Position, &c.Revision, &c.Raw)
	if e == sql.ErrNoRows {
		return c, ErrNotFound
	}
	if e != nil {
		return c, e
	}
	c.Segments = []Segment{}
	rows, e := s.DB.Query(`SELECT s.id,s.chapter_id,s.position,s.paragraph,s.start,s.end,s.text,s.speech_text,s.speech_revision,coalesce(t.text,''),coalesce(t.revision,0),coalesce(t.manual,0) FROM segments s LEFT JOIN translations t ON t.segment_id=s.id WHERE s.chapter_id=? ORDER BY s.position`, identity)
	if e != nil {
		return c, e
	}
	for rows.Next() {
		var seg Segment
		if e = rows.Scan(&seg.ID, &seg.ChapterID, &seg.Position, &seg.Paragraph, &seg.Start, &seg.End, &seg.Text, &seg.SpeechText, &seg.SpeechRevision, &seg.Translation, &seg.TranslationRevision, &seg.Manual); e != nil {
			rows.Close()
			return c, e
		}
		seg.Clips = []Clip{}
		c.Segments = append(c.Segments, seg)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return c, e
	}
	for i := range c.Segments {
		rows, e = s.DB.Query(`SELECT a.id,a.segment_id,a.position,a.text,coalesce(a.asset_id,''),coalesce(r.duration,0) FROM audio_clips a LEFT JOIN audio_assets r ON r.id=a.asset_id WHERE segment_id=? ORDER BY a.position`, c.Segments[i].ID)
		if e != nil {
			return c, e
		}
		for rows.Next() {
			var clip Clip
			if e = rows.Scan(&clip.ID, &clip.SegmentID, &clip.Position, &clip.Text, &clip.AssetID, &clip.Duration); e != nil {
				rows.Close()
				return c, e
			}
			c.Segments[i].Clips = append(c.Segments[i].Clips, clip)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return c, e
		}
	}
	return c, nil
}
func (s *Store) EditSource(identity, title, raw string, revision int) error {
	if e := validateSource(title, raw); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old string
	var rev int
	if e = tx.QueryRow("SELECT raw,revision FROM chapters WHERE id=?", identity).Scan(&old, &rev); e == sql.ErrNoRows {
		return ErrNotFound
	} else if e != nil {
		return e
	}
	if rev != revision {
		return ErrConflict
	}
	if old == raw {
		_, e = tx.Exec("UPDATE chapters SET title=? WHERE id=?", title, identity)
		if e != nil {
			return e
		}
		return tx.Commit()
	}
	_, e = tx.Exec(`INSERT INTO translation_history SELECT lower(hex(randomblob(16))),s.chapter_id,s.id,s.text,t.text,t.manual,? FROM translations t JOIN segments s ON s.id=t.segment_id WHERE s.chapter_id=?`, now(), identity)
	if e != nil {
		return e
	}
	for _, q := range []struct {
		q string
		a []any
	}{{"UPDATE chapters SET raw=?,title=?,revision=revision+1 WHERE id=?", []any{raw, title, identity}}, {"INSERT INTO chapter_revisions VALUES(?,?,?,?)", []any{identity, rev + 1, raw, now()}}, {"DELETE FROM segments WHERE chapter_id=?", []any{identity}}, {"DELETE FROM reading_progress WHERE chapter_id=?", []any{identity}}} {
		if _, e = tx.Exec(q.q, q.a...); e != nil {
			return e
		}
	}
	if e = insertSegments(tx, identity, raw); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) EditTranslation(seg, text string, revision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = saveManualTranslation(tx, "", seg, text, revision); e != nil {
		return e
	}
	return tx.Commit()
}

type ManualTranslationEdit struct {
	SegmentID string `json:"segment_id"`
	Text      string `json:"text"`
	Revision  int    `json:"revision"`
}

// Save only submitted paragraphs. All edits share a transaction so a stale
// paragraph cannot leave a chapter half saved. No provider is involved.
func (s *Store) EditChapterTranslations(chapter string, revision int, edits []ManualTranslationEdit) error {
	if len(edits) == 0 || len(edits) > 5000 {
		return fmt.Errorf("每次请提交 1–5000 段译文")
	}
	seen := map[string]bool{}
	for _, edit := range edits {
		if seen[edit.SegmentID] {
			return fmt.Errorf("译文段落重复")
		}
		seen[edit.SegmentID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var current int
	if e = tx.QueryRow("SELECT revision FROM chapters WHERE id=?", chapter).Scan(&current); e == sql.ErrNoRows {
		return ErrNotFound
	} else if e != nil {
		return e
	}
	if current != revision {
		return ErrConflict
	}
	for _, edit := range edits {
		if e = saveManualTranslation(tx, chapter, edit.SegmentID, edit.Text, edit.Revision); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func saveManualTranslation(tx *sql.Tx, expectedChapter, seg, text string, revision int) error {
	var current int
	var chapter, source string
	e := tx.QueryRow("SELECT s.chapter_id,s.text,coalesce(t.revision,0) FROM segments s LEFT JOIN translations t ON t.segment_id=s.id WHERE s.id=?", seg).Scan(&chapter, &source, &current)
	if e == sql.ErrNoRows {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if expectedChapter != "" && chapter != expectedChapter {
		return fmt.Errorf("段落不属于当前章节")
	}
	if current != revision {
		return ErrConflict
	}
	if strings.TrimSpace(text) == "" || len([]rune(text)) > 20000 {
		return fmt.Errorf("译文不能为空或超过 20000 字")
	}
	_, e = tx.Exec("INSERT INTO translation_history SELECT ?,?,segment_id,?,text,manual,? FROM translations WHERE segment_id=?", id(), chapter, source, now(), seg)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO translations VALUES(?,?,?,1,'manual','') ON CONFLICT(segment_id) DO UPDATE SET text=excluded.text,revision=excluded.revision,manual=1", seg, text, current+1)
	if e != nil {
		return e
	}
	return nil
}
func (s *Store) EditSpeech(seg, text string, revision int) error {
	if strings.TrimSpace(text) == "" || len([]rune(text)) > 4000 {
		return fmt.Errorf("朗读文本须为 1–4000 字")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old string
	var rev int
	e = tx.QueryRow("SELECT speech_text,speech_revision FROM segments WHERE id=?", seg).Scan(&old, &rev)
	if e == sql.ErrNoRows {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if rev != revision {
		return ErrConflict
	}
	if old == text {
		return nil
	}
	if _, e = tx.Exec("UPDATE segments SET speech_text=?,speech_revision=speech_revision+1 WHERE id=?", text, seg); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM audio_clips WHERE segment_id=?", seg); e != nil {
		return e
	}
	if e = insertClips(tx, seg, text); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE reading_progress SET clip_id='',seconds=0,revision=revision+1 WHERE segment_id=? AND clip_id!=''", seg); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Glossary(book string) (Glossary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.glossary(book)
}
func (s *Store) glossary(book string) (Glossary, error) {
	g := Glossary{Terms: []Term{}}
	if e := s.DB.QueryRow("SELECT glossary_revision FROM books WHERE id=?", book).Scan(&g.Revision); e != nil {
		return g, e
	}
	rows, e := s.DB.Query("SELECT source,target,reading FROM glossary_entries WHERE book_id=? ORDER BY source", book)
	if e != nil {
		return g, e
	}
	defer rows.Close()
	for rows.Next() {
		var t Term
		if e = rows.Scan(&t.Source, &t.Target, &t.Reading); e != nil {
			return g, e
		}
		g.Terms = append(g.Terms, t)
	}
	return g, rows.Err()
}
func (s *Store) SaveGlossary(book string, g Glossary) error {
	if len(g.Terms) > 500 {
		return fmt.Errorf("术语不能超过 500 项")
	}
	seen := map[string]bool{}
	for _, t := range g.Terms {
		if strings.TrimSpace(t.Source) == "" || strings.TrimSpace(t.Target) == "" || len([]rune(t.Source+t.Target+t.Reading)) > 500 || seen[t.Source] {
			return fmt.Errorf("术语必须唯一且原文、译名非空，每项最多 500 字")
		}
		seen[t.Source] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.Exec("UPDATE books SET glossary_revision=glossary_revision+1 WHERE id=? AND glossary_revision=?", book, g.Revision)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if _, e = tx.Exec("DELETE FROM glossary_entries WHERE book_id=?", book); e != nil {
		return e
	}
	for _, t := range g.Terms {
		if _, e = tx.Exec("INSERT INTO glossary_entries VALUES(?,?,?,?)", book, t.Source, t.Target, t.Reading); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Progress(book string) (Progress, error) {
	p := Progress{}
	e := s.DB.QueryRow("SELECT chapter_id,segment_id,clip_id,seconds,revision FROM reading_progress WHERE book_id=?", book).Scan(&p.ChapterID, &p.SegmentID, &p.ClipID, &p.Seconds, &p.Revision)
	if e == sql.ErrNoRows {
		return p, nil
	}
	return p, e
}
func (s *Store) SaveProgress(book string, p Progress) (Progress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	var valid int
	e = tx.QueryRow("SELECT count(*) FROM segments s JOIN chapters c ON c.id=s.chapter_id WHERE c.book_id=? AND c.id=? AND s.id=?", book, p.ChapterID, p.SegmentID).Scan(&valid)
	if e != nil {
		return p, e
	}
	if valid != 1 || p.Seconds < 0 || p.Seconds > 3600 {
		return p, fmt.Errorf("阅读位置无效")
	}
	if p.ClipID != "" {
		if e = tx.QueryRow("SELECT count(*) FROM audio_clips WHERE id=? AND segment_id=?", p.ClipID, p.SegmentID).Scan(&valid); e != nil {
			return p, e
		}
		if valid != 1 {
			return p, fmt.Errorf("播放位置无效")
		}
	}
	var current int
	e = tx.QueryRow("SELECT revision FROM reading_progress WHERE book_id=?", book).Scan(&current)
	if e != nil && e != sql.ErrNoRows {
		return p, e
	}
	if current != p.Revision {
		return p, ErrConflict
	}
	p.Revision++
	_, e = tx.Exec("INSERT INTO reading_progress VALUES(?,?,?,?,?,?) ON CONFLICT(book_id) DO UPDATE SET chapter_id=excluded.chapter_id,segment_id=excluded.segment_id,clip_id=excluded.clip_id,seconds=excluded.seconds,revision=excluded.revision", book, p.ChapterID, p.SegmentID, p.ClipID, p.Seconds, p.Revision)
	if e != nil {
		return p, e
	}
	return p, tx.Commit()
}
