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
		if r.URL.Path != "/audio/speech" || body["voice"] != "Kore" || body["response_format"] != "pcm" || body["input"] != "朝の風が吹いた。" {
			t.Error("speech payload changed transcript or voice")
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
	c.Voice = "Kore"
	raw, err := (&Provider{Config: c}).Synthesize(context.Background(), "朝の風が吹いた。")
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := parseWAV(raw)
	if err != nil || pcm.Rate != 24000 {
		t.Fatal("invalid PCM wrapping", err)
	}
}
