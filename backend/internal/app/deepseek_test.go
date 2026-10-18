package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepSeekChatProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("wrong endpoint %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing auth")
		}
		var body struct {
			Model          string              `json:"model"`
			Stream         bool                `json:"stream"`
			MaxTokens      int                 `json:"max_tokens"`
			ResponseFormat map[string]string   `json:"response_format"`
			Thinking       map[string]string   `json:"thinking"`
			Messages       []map[string]string `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ResponseFormat["type"] != "json_object" || body.Thinking["type"] != "disabled" || body.MaxTokens != 12000 || body.Stream || len(body.Messages) != 2 || !strings.Contains(body.Messages[0]["content"], "JSON") {
			t.Errorf("unexpected chat payload: %+v", body)
		}
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"items\":[{\"segment_id\":\"a\",\"translation\":\"窗外是晴天。\"}],\"summary\":\"天气晴朗\"}"}}],"usage":{"prompt_tokens":9,"completion_tokens":6}}`)
	}))
	defer server.Close()
	c := testConfig(t)
	c.LLMAPIStyle = "chat_completions"
	c.Model = "deepseek-flash"
	c.ResponsesURL = server.URL
	p := &Provider{Config: c}
	result, err := p.Translate(context.Background(), ItemPayload{Targets: []Target{{ID: "a", Text: "窓の外は晴れている。"}}})
	if err != nil || len(result.Items) != 1 || len(result.Usage) == 0 {
		t.Fatal(result, err)
	}
	original := c
	c.LLMAPIStyle = "responses"
	if c.translationKey() == original.translationKey() {
		t.Fatal("protocol missing from task config fingerprint")
	}
}
func TestChatRejectsTruncationRefusalAndMissingSegments(t *testing.T) {
	for _, sample := range []string{
		`{"choices":[{"finish_reason":"length","message":{"content":"{}"}}],"usage":{"completion_tokens":7}}`,
		`{"choices":[{"finish_reason":"content_filter","message":{"content":""}}],"usage":{"completion_tokens":7}}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"{\"items\":[],\"summary\":\"\"}"}}],"usage":{"completion_tokens":7}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sample) }))
		c := testConfig(t)
		c.LLMAPIStyle = "chat_completions"
		c.ResponsesURL = server.URL
		result, err := (&Provider{Config: c}).Translate(context.Background(), ItemPayload{Targets: []Target{{ID: "a", Text: "雨。"}}})
		server.Close()
		if err == nil || len(result.Usage) == 0 {
			t.Fatal("invalid output or usage lost", err)
		}
	}
}
func TestGeminiOpenRouterFemaleVoiceAndMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/audio/speech" || body["voice"] != "Aoede" || body["response_format"] != "pcm" || body["input"] != "朝の風が吹いた。" {
			t.Error("speech payload changed transcript or voice")
		}
		if body["speed"] != float64(1) {
			t.Error("speech synthesis must default to original speed 1.0")
		}
		options := body["provider"].(map[string]any)["options"].(map[string]any)
		metadata := options["google-ai-studio"].(map[string]any)["speech_metadata"].(map[string]any)
		if metadata["style"] == nil {
			t.Error("missing separate delivery instructions")
		}
		w.Header().Set("Content-Type", "audio/pcm;rate=24000;channels=1")
		w.Write(make([]byte, 480))
	}))
	defer server.Close()
	c := testConfig(t)
	c.TTSProvider = "openrouter"
	c.TTSURL = server.URL
	c.TTSModel = "google/gemini-3.8-flash-tts"
	c.Voice = "Aoede"
	raw, err := (&Provider{Config: c}).Synthesize(context.Background(), "朝の風が吹いた。")
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := parseWAV(raw)
	if err != nil || pcm.Rate != 24000 {
		t.Fatal("invalid PCM wrapping", err)
	}
}

func TestDeepSeekResponsesProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("unexpected Responses endpoint/auth")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["store"] != false || body["messages"] != nil || body["thinking"] != nil || body["max_tokens"] != nil || body["input"] == nil || body["instructions"] == nil {
			t.Error("Responses request must not contain Chat Completions fields")
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		text, _ := body["text"].(map[string]any)
		format, _ := text["format"].(map[string]any)
		if reasoning["effort"] != "none" || format["type"] != "json_schema" || format["schema"] == nil || format["name"] != "novel_translation" || body["max_output_tokens"] != float64(12000) {
			t.Error("Responses reasoning/schema settings missing")
		}
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"reasoning","content":[{"type":"reasoning_text","text":"not part of the translation"}]},{"type":"message","content":[{"type":"output_text","text":"{\"items\":[{\"segment_id\":\"a\",\"translation\":\"窗外是晴天。\"}],\"summary\":\"天气晴朗\"}"}]}],"usage":{"input_tokens":9,"output_tokens":6}}`)
	}))
	defer server.Close()
	c := testConfig(t)
	c.Model, c.ResponsesURL = "deepseek-flash", server.URL
	// An unset style must follow the same Responses path as the explicit default.
	for _, style := range []string{"", "responses"} {
		c.LLMAPIStyle = style
		r, err := (&Provider{Config: c}).Translate(context.Background(), ItemPayload{Targets: []Target{{ID: "a", Text: "窓の外は晴れている。"}}})
		if err != nil || len(r.Items) != 1 || r.Items[0].Translation != "窗外是晴天。" || len(r.Usage) == 0 {
			t.Fatal(r, err)
		}
	}
}

func TestResponsesRejectsInvalidOutputWithoutProtocolFallback(t *testing.T) {
	for _, sample := range []string{
		`{"status":"incomplete","output":[],"usage":{"output_tokens":7}}`,
		`{"status":"completed","output":[{"content":[{"type":"refusal","text":"refused"}]}],"usage":{"output_tokens":7}}`,
		`{"status":"completed","output":[{"content":[{"type":"output_text","text":"{\"items\":[],\"summary\":\"\"}"}]}],"usage":{"output_tokens":7}}`,
	} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/responses" {
				t.Error("unexpected protocol fallback")
			}
			fmt.Fprint(w, sample)
		}))
		c := testConfig(t)
		c.Model = "deepseek-flash"
		c.ResponsesURL = server.URL
		r, err := (&Provider{Config: c}).Translate(context.Background(), ItemPayload{Targets: []Target{{ID: "a", Text: "雨。"}}})
		server.Close()
		if err == nil || len(r.Usage) == 0 || calls != 1 {
			t.Fatal("invalid output accepted, usage lost, or request repeated", err, calls)
		}
	}
}

func TestDefaultResponsesAndGeminiVoice(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("LLM_API_STYLE", "")
	t.Setenv("TTS_PROVIDER", "openrouter")
	t.Setenv("TTS_MODEL", "google/gemini-3.8-flash-tts")
	t.Setenv("TTS_VOICE", "")
	t.Setenv("TTS_CONCURRENCY", "")
	c := LoadConfig()
	if c.LLMAPIStyle != "responses" || c.Voice != "Aoede" {
		t.Fatal("wrong default protocol or voice")
	}
	if c.TTSConcurrency != 10 {
		t.Fatal("default TTS concurrency must be 10")
	}
	t.Setenv("TTS_VOICE", "Sulafat")
	if LoadConfig().Voice != "Sulafat" {
		t.Fatal("explicit voice overridden")
	}
	t.Setenv("TTS_VOICE", "")
	t.Setenv("TTS_PROVIDER", "openai")
	t.Setenv("TTS_MODEL", "gpt-4o-mini-tts")
	if LoadConfig().Voice != "marin" {
		t.Fatal("OpenAI voice default changed")
	}
	old := c
	old.Voice = "Kore"
	if old.speechKey() == c.speechKey() {
		t.Fatal("changing voice must change audio cache key")
	}
}
