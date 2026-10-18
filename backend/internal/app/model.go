package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrConflict = errors.New("内容版本已变化，请刷新后重试")
var ErrNotFound = errors.New("内容不存在")

func id() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type Book struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Author           string `json:"author"`
	Created          string `json:"created"`
	ChapterCount     int    `json:"chapter_count"`
	GlossaryRevision int    `json:"glossary_revision"`
}
type Chapter struct {
	ID         string    `json:"id"`
	BookID     string    `json:"book_id"`
	Title      string    `json:"title"`
	Position   int       `json:"position"`
	Revision   int       `json:"revision"`
	Raw        string    `json:"raw"`
	Segments   []Segment `json:"segments"`
	Translated int       `json:"translated"`
	AudioReady int       `json:"audio_ready"`
	Total      int       `json:"total"`
}
type Segment struct {
	ID                  string `json:"id"`
	ChapterID           string `json:"chapter_id"`
	Position            int    `json:"position"`
	Paragraph           int    `json:"paragraph"`
	Start               int    `json:"start"`
	End                 int    `json:"end"`
	Text                string `json:"text"`
	Translation         string `json:"translation"`
	TranslationRevision int    `json:"translation_revision"`
	Manual              bool   `json:"manual"`
	SpeechText          string `json:"speech_text"`
	SpeechRevision      int    `json:"speech_revision"`
	Clips               []Clip `json:"clips"`
}
type Clip struct {
	ID        string  `json:"id"`
	SegmentID string  `json:"segment_id"`
	Position  int     `json:"position"`
	Text      string  `json:"text"`
	AssetID   string  `json:"asset_id"`
	Duration  float64 `json:"duration"`
}
type Term struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Reading string `json:"reading"`
}
type Glossary struct {
	Revision int    `json:"revision"`
	Terms    []Term `json:"terms"`
}
type Progress struct {
	ChapterID string  `json:"chapter_id"`
	SegmentID string  `json:"segment_id"`
	ClipID    string  `json:"clip_id"`
	Seconds   float64 `json:"seconds"`
	Revision  int     `json:"revision"`
}
type Job struct {
	ID        string    `json:"id"`
	ChapterID string    `json:"chapter_id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Revision  int       `json:"revision"`
	Done      int       `json:"done"`
	Total     int       `json:"total"`
	Message   string    `json:"message"`
	Created   string    `json:"created"`
	Cancel    bool      `json:"cancel"`
	Items     []JobItem `json:"items,omitempty"`
}
type JobItem struct {
	ID       string `json:"id"`
	JobID    string `json:"job_id"`
	Position int    `json:"position"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Payload  string `json:"-"`
}
type Target struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Version int    `json:"version"`
}
type ItemPayload struct {
	Targets          []Target `json:"targets"`
	Context          string   `json:"context"`
	Terms            []Term   `json:"terms"`
	GlossaryRevision int      `json:"glossary_revision"`
	ConfigKey        string   `json:"config_key"`
	Force            bool     `json:"force"`
}

// Split preserves every rune in each piece. Source offsets use rune indices, never UTF-16.
func Split(text string, limit int) []string {
	if limit < 1 {
		limit = 280
	}
	rs := []rune(text)
	out := []string{}
	for len(rs) > limit {
		cut := 0
		for i := 0; i < limit; i++ {
			if strings.ContainsRune("。！？!?\n", rs[i]) {
				cut = i + 1
				for cut < limit && strings.ContainsRune("」』）”’", rs[cut]) {
					cut++
				}
			}
		}
		if cut == 0 {
			for i := limit - 1; i >= limit/2; i-- {
				if strings.ContainsRune("、，,　 ", rs[i]) {
					cut = i + 1
					break
				}
			}
		}
		if cut == 0 {
			cut = limit
		}
		out = append(out, string(rs[:cut]))
		rs = rs[cut:]
	}
	if len(rs) > 0 {
		out = append(out, string(rs))
	}
	return out
}
func segments(raw string) []Segment {
	result := []Segment{}
	offset := 0
	paragraph := 0
	for _, line := range strings.SplitAfter(raw, "\n") {
		length := len([]rune(line))
		if strings.TrimSpace(line) != "" {
			for _, part := range Split(line, 900) {
				result = append(result, Segment{ID: id(), Position: len(result), Paragraph: paragraph, Start: offset, End: offset + len([]rune(part)), Text: part, SpeechText: strings.TrimSpace(part), SpeechRevision: 1, Clips: []Clip{}})
				offset += len([]rune(part))
			}
			paragraph++
		} else {
			offset += length
		}
	}
	return result
}
