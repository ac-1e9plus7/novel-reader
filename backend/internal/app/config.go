package app

import (
	"github.com/joho/godotenv"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	LLMAPIStyle                                                                             string
	Addr, DataDir, FrontendDir, ResponsesURL, ResponsesKey, Model                           string
	TTSProvider, TTSURL, TTSKey, TTSModel, Voice, AzureKey, AzureRegion, AzureVoice, FFmpeg string
	Structured                                                                              bool
	ResponseTimeout, TTSTimeout                                                             time.Duration
	BatchChars, TTSConcurrency                                                              int
}

func env(k, d string) string {
	if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return d
}
func envInt(k string, d, min, max int) int {
	n, e := strconv.Atoi(env(k, strconv.Itoa(d)))
	if e != nil || n < min || n > max {
		return d
	}
	return n
}
func LoadConfig() Config {
	_ = godotenv.Load()
	return Config{
		LLMAPIStyle: env("LLM_API_STYLE", "responses"),
		Addr:        env("LISTEN_ADDR", "127.0.0.1:8787"), DataDir: env("DATA_DIR", "data"), FrontendDir: env("FRONTEND_DIR", "frontend/dist"),
		ResponsesURL: strings.TrimRight(env("RESPONSES_BASE_URL", "https://api.openai.com/v1"), "/"), ResponsesKey: env("RESPONSES_API_KEY", ""), Model: env("RESPONSES_MODEL", ""), Structured: env("RESPONSES_STRUCTURED_OUTPUT", "true") == "true", ResponseTimeout: time.Duration(envInt("RESPONSES_TIMEOUT_SECONDS", 300, 5, 1800)) * time.Second,
		TTSProvider: env("TTS_PROVIDER", "openai"), TTSURL: strings.TrimRight(env("TTS_BASE_URL", "https://api.openai.com/v1"), "/"), TTSKey: env("TTS_API_KEY", ""), TTSModel: env("TTS_MODEL", "gpt-4o-mini-tts"), Voice: env("TTS_VOICE", "marin"), AzureKey: env("AZURE_SPEECH_KEY", ""), AzureRegion: env("AZURE_SPEECH_REGION", ""), AzureVoice: env("AZURE_SPEECH_VOICE", "ja-JP-NanamiNeural"), TTSTimeout: time.Duration(envInt("TTS_TIMEOUT_SECONDS", 180, 5, 1800)) * time.Second,
		BatchChars: envInt("TRANSLATION_BATCH_CHARS", 2400, 900, 12000), TTSConcurrency: envInt("TTS_CONCURRENCY", 2, 1, 8), FFmpeg: env("FFMPEG_PATH", "ffmpeg"),
	}
}
func (c Config) translationReady() bool {
	return c.ResponsesKey != "" && c.Model != "" && (c.LLMAPIStyle == "" || c.LLMAPIStyle == "responses" || c.LLMAPIStyle == "chat_completions")
}
func (c Config) speechReady() bool {
	if c.TTSProvider == "azure" {
		return c.AzureKey != "" && c.AzureRegion != ""
	}
	return (c.TTSProvider == "openai" || c.TTSProvider == "openrouter") && c.TTSKey != ""
}
