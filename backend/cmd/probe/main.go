// Explicit paid smoke probe. Fixed neutral sample; never imports a novel chapter.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"novel-reader/internal/app"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	confirm := flag.Bool("confirm-paid", false, "Allow one translation and one TTS job for a fixed neutral sample")
	output := flag.String("out", "data/probes/"+time.Now().UTC().Format("20060102-150405"), "New isolated output directory")
	flag.Parse()
	if !*confirm {
		return fmt.Errorf("真实验证可能产生费用；明确授权后使用 -confirm-paid")
	}
	if _, err := os.Stat(*output); err == nil {
		return fmt.Errorf("输出目录已经存在，请使用新目录，避免重复付费或覆盖结果")
	}
	c := app.LoadConfig()
	c.DataDir = *output
	s, err := app.NewServer(c, nil, nil)
	if err != nil {
		return err
	}
	defer s.Close()
	b, err := s.Store.CreateBook("接口验证样本（非小说章节）", "")
	if err != nil {
		return err
	}
	const source = "朝の光が窓から差し込んだ。私は本を開き、静かな部屋で新しい物語を読み始めた。"
	cid, err := s.Store.CreateChapter(b.ID, "中性短文验证", source)
	if err != nil {
		return err
	}
	report := map[string]any{"created": time.Now().UTC().Format(time.RFC3339), "model": c.Model, "llm_api_style": c.LLMAPIStyle, "tts_model": c.TTSModel, "voice": c.Voice, "source": source}
	failed := false
	chapterInput, err := s.Store.Chapter(cid)
	if err != nil {
		return err
	}
	for _, kind := range []string{"translation", "audio"} {
		started := time.Now()
		var selected []string
		if kind == "translation" {
			selected = []string{chapterInput.Segments[0].ID}
		}
		j, e := s.Engine.Create(cid, kind, 1, selected, false)
		if e != nil {
			return e
		}
		deadline := time.Now().Add(c.ResponseTimeout + c.TTSTimeout + time.Minute)
		for time.Now().Before(deadline) {
			j, e = s.Store.Job(j.ID)
			if e != nil {
				return e
			}
			if j.Status != "queued" && j.Status != "running" {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		report[kind] = map[string]any{"status": j.Status, "elapsed_ms": time.Since(started).Milliseconds(), "job_id": j.ID, "items": j.Items}
		fmt.Printf("%s: %s (%d ms)\n", kind, j.Status, time.Since(started).Milliseconds())
		if j.Status != "succeeded" {
			failed = true
			for _, item := range j.Items {
				if item.Message != "" {
					fmt.Println(item.Message)
				}
			}
		}
	}
	chapter, err := s.Store.Chapter(cid)
	if err != nil {
		return err
	}
	report["translation_text"] = chapter.Segments[0].Translation
	clip := chapter.Segments[0].Clips[0]
	if clip.AssetID != "" {
		raw, _, e := s.Audio.Read(clip.AssetID, true)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(*output, "sample.wav"), raw, 0600); e != nil {
			return e
		}
		report["audio_seconds"] = clip.Duration
		deadline := time.Now().Add(15 * time.Second)
		format := "wav"
		for time.Now().Before(deadline) {
			_, format, err = s.Audio.Read(clip.AssetID, false)
			if err != nil {
				return err
			}
			if format == "flac" {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		report["stored_format"] = format
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(filepath.Join(*output, "report.json"), encoded, 0600); err != nil {
		return err
	}
	fmt.Println("Saved isolated report and sample to", *output)
	if failed {
		return fmt.Errorf("部分真实接口验证失败，详见脱敏报告")
	}
	return nil
}
