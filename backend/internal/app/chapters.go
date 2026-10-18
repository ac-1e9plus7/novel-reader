package app

import (
	"database/sql"
	"fmt"
	"slices"
)

func chapterOrder(tx *sql.Tx, book string) ([]string, error) {
	rows, e := tx.Query("SELECT id FROM chapters WHERE book_id=? ORDER BY position,id", book)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func saveChapterOrder(tx *sql.Tx, ids []string) error {
	for position, id := range ids {
		if _, e := tx.Exec("UPDATE chapters SET position=? WHERE id=?", position, id); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) ReorderChapters(book string, expected, ordered []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRow("SELECT count(*) FROM books WHERE id=?", book).Scan(&exists); e != nil {
		return e
	}
	if exists != 1 {
		return ErrNotFound
	}
	current, e := chapterOrder(tx, book)
	if e != nil {
		return e
	}
	if !slices.Equal(current, expected) {
		return ErrConflict
	}
	if len(ordered) != len(current) {
		return fmt.Errorf("排序必须包含当前小说的全部章节")
	}
	allowed := map[string]bool{}
	for _, id := range current {
		allowed[id] = true
	}
	for _, id := range ordered {
		if !allowed[id] {
			return fmt.Errorf("排序包含重复或不属于当前小说的章节")
		}
		delete(allowed, id)
	}
	if e = saveChapterOrder(tx, ordered); e != nil {
		return e
	}
	return tx.Commit()
}

// Delete dependent text and job records together; audio assets stay available
// to other chapters and are reclaimed only by explicit cache cleanup.
func (s *Store) DeleteChapter(chapter string, revision int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var book string
	var current int
	e = tx.QueryRow("SELECT book_id,revision FROM chapters WHERE id=?", chapter).Scan(&book, &current)
	if e == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if current != revision {
		return nil, ErrConflict
	}
	rows, e := tx.Query("SELECT id FROM jobs WHERE chapter_id=? AND status IN ('queued','running')", chapter)
	if e != nil {
		return nil, e
	}
	jobs := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		jobs = append(jobs, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec("DELETE FROM reading_progress WHERE chapter_id=?", chapter); e != nil {
		return nil, e
	}
	if _, e = tx.Exec("DELETE FROM chapters WHERE id=?", chapter); e != nil {
		return nil, e
	}
	ids, e := chapterOrder(tx, book)
	if e != nil {
		return nil, e
	}
	if e = saveChapterOrder(tx, ids); e != nil {
		return nil, e
	}
	return jobs, tx.Commit()
}
func (e *Engine) DeleteChapter(chapter string, revision int) error {
	jobs, err := e.Store.DeleteChapter(chapter, revision)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range jobs {
		if cancel := e.cancels[id]; cancel != nil {
			cancel()
		}
	}
	return nil
}
