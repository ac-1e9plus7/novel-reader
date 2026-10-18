package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/mewkiz/flac"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type PCM struct {
	Data           []byte
	Rate, Channels int
}

func (a *Audio) assetExists(path string) bool {
	_, e := os.Stat(filepath.Join(a.Store.Root, "audio", path))
	return e == nil
}
func validPCM(rate, channels int, data []byte) bool {
	return rate >= 8000 && rate <= 192000 && (channels == 1 || channels == 2) && len(data) > 0 && len(data)%(channels*2) == 0 && len(data) <= rate*channels*2*300
}
func makeWAV(data []byte, rate, channels int) []byte {
	b := new(bytes.Buffer)
	b.WriteString("RIFF")
	binary.Write(b, binary.LittleEndian, uint32(len(data)+36))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(channels), uint32(rate), uint32(rate * channels * 2), uint16(channels * 2), uint16(16)} {
		binary.Write(b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}
func parseWAV(raw []byte) (PCM, error) {
	p := PCM{}
	bad := fmt.Errorf("语音不是有效的 16 位 PCM WAV")
	if len(raw) < 44 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return p, bad
	}
	format := false
	for off := 12; off+8 <= len(raw); {
		kind := string(raw[off : off+4])
		size := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		off += 8
		if size > len(raw)-off {
			if kind == "data" && (size == int(^uint32(0)) || size >= 0x7ffff000) {
				size = len(raw) - off
			} else {
				return p, bad
			}
		}
		if kind == "fmt " {
			if size < 16 || binary.LittleEndian.Uint16(raw[off:]) != 1 || binary.LittleEndian.Uint16(raw[off+14:]) != 16 {
				return p, bad
			}
			p.Channels = int(binary.LittleEndian.Uint16(raw[off+2:]))
			p.Rate = int(binary.LittleEndian.Uint32(raw[off+4:]))
			format = true
		}
		if kind == "data" {
			p.Data = raw[off : off+size]
		}
		off += size + (size % 2)
	}
	if !format || !validPCM(p.Rate, p.Channels, p.Data) {
		return p, bad
	}
	return p, nil
}
func normalizeWAV(raw []byte) ([]byte, error) {
	p, e := parseWAV(raw)
	if e != nil {
		return nil, e
	}
	return makeWAV(p.Data, p.Rate, p.Channels), nil
}
func decodeFLAC(raw []byte) ([]byte, error) {
	stream, e := flac.New(bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	rate, channels := int(stream.Info.SampleRate), int(stream.Info.NChannels)
	if stream.Info.BitsPerSample != 16 || rate < 8000 || rate > 192000 || channels < 1 || channels > 2 {
		return nil, fmt.Errorf("不支持的 FLAC 格式")
	}
	var pcm bytes.Buffer
	for {
		frame, e := stream.ParseNext()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if len(frame.Subframes) != channels {
			return nil, fmt.Errorf("FLAC 声道损坏")
		}
		for i := range frame.Subframes[0].Samples {
			for ch := 0; ch < channels; ch++ {
				if i >= len(frame.Subframes[ch].Samples) {
					return nil, fmt.Errorf("FLAC 样本损坏")
				}
				binary.Write(&pcm, binary.LittleEndian, int16(frame.Subframes[ch].Samples[i]))
			}
		}
		if pcm.Len() > rate*channels*2*300 {
			return nil, fmt.Errorf("音频过长")
		}
	}
	if !validPCM(rate, channels, pcm.Bytes()) {
		return nil, fmt.Errorf("FLAC 音频为空")
	}
	return makeWAV(pcm.Bytes(), rate, channels), nil
}
func ffmpeg(ctx context.Context, path string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, args...)...)
	hideWindow(cmd)
	cmd.Stdin = bytes.NewReader(input)
	var output, stderr bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	if e := cmd.Run(); e != nil {
		return nil, fmt.Errorf("音频转码失败，请检查 FFmpeg")
	}
	return output.Bytes(), nil
}
func decodeWithFFmpeg(ctx context.Context, path string, raw []byte) ([]byte, error) {
	out, e := ffmpeg(ctx, path, raw, "-i", "pipe:0", "-t", "300", "-map", "0:a:0", "-acodec", "pcm_s16le", "-f", "wav", "pipe:1")
	if e != nil {
		return nil, e
	}
	return normalizeWAV(out)
}

type audioLock struct {
	mu   sync.Mutex
	refs int
}
type Audio struct {
	Store      *Store
	Config     Config
	Synth      Synthesizer
	keysMu     sync.Mutex
	keys       map[string]*audioLock
	limiter    *synthesisLimiter
	settingsMu sync.Mutex
	compress   chan string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	enabled    bool
}

func NewAudio(s *Store, c Config, synth Synthesizer) *Audio {
	ctx, cancel := context.WithCancel(context.Background())
	a := &Audio{Store: s, Config: c, Synth: synth, keys: map[string]*audioLock{}, limiter: newSynthesisLimiter(c.TTSConcurrency), compress: make(chan string, 128), ctx: ctx, cancel: cancel}
	probe, cancelProbe := context.WithTimeout(ctx, 10*time.Second)
	raw := makeWAV(make([]byte, 480), 24000, 1)
	encoded, e := ffmpeg(probe, c.FFmpeg, raw, "-i", "pipe:0", "-c:a", "flac", "-f", "flac", "pipe:1")
	cancelProbe()
	if e == nil {
		_, e = decodeFLAC(encoded)
	}
	a.enabled = e == nil
	if !a.enabled {
		log.Print("FFmpeg FLAC 压缩不可用，继续保存 WAV")
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case asset := <-a.compress:
				if e := a.compressAsset(asset); e != nil {
					log.Printf("音频压缩未完成，保留 WAV: %v", e)
				}
			}
		}
	}()
	return a
}
func (a *Audio) Close() { a.cancel(); a.wg.Wait() }
func (a *Audio) schedule(asset string) {
	if a.enabled {
		select {
		case a.compress <- asset:
		default:
		}
	}
}
func (a *Audio) Generate(ctx context.Context, text string, force bool) (string, error) {
	key := digest([]string{a.Config.speechKey(), text})
	a.keysMu.Lock()
	lock := a.keys[key]
	if lock == nil {
		lock = &audioLock{}
		a.keys[key] = lock
	}
	lock.refs++
	a.keysMu.Unlock()
	lock.mu.Lock()
	defer func() {
		lock.mu.Unlock()
		a.keysMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(a.keys, key)
		}
		a.keysMu.Unlock()
	}()
	if e := ctx.Err(); e != nil {
		return "", e
	}
	if !force {
		a.Store.mu.Lock()
		var asset, path string
		e := a.Store.DB.QueryRow("SELECT id,path FROM audio_assets WHERE cache_key=? ORDER BY created DESC LIMIT 1", key).Scan(&asset, &path)
		if e == nil {
			_, e = os.Stat(filepath.Join(a.Store.Root, "audio", path))
		}
		a.Store.mu.Unlock()
		if e == nil {
			a.schedule(asset)
			return asset, nil
		}
	}
	if err := a.limiter.acquire(ctx); err != nil {
		return "", err
	}
	raw, e := a.Synth.Synthesize(ctx, text)
	a.limiter.release()
	if e != nil {
		return "", e
	}
	raw, e = normalizeWAV(raw)
	if e != nil {
		return "", e
	}
	pcm, _ := parseWAV(raw)
	asset := id()
	name := asset + ".wav"
	temp := filepath.Join(a.Store.Root, "audio", asset+".tmp")
	if e = os.WriteFile(temp, raw, 0600); e != nil {
		return "", e
	}
	defer os.Remove(temp)
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	if e = os.Rename(temp, filepath.Join(a.Store.Root, "audio", name)); e != nil {
		return "", e
	}
	_, e = a.Store.DB.Exec("INSERT INTO audio_assets VALUES(?,?,?,?,?,?)", asset, key, name, "wav", float64(len(pcm.Data))/float64(pcm.Rate*pcm.Channels*2), now())
	if e != nil {
		os.Remove(filepath.Join(a.Store.Root, "audio", name))
		return "", e
	}
	a.schedule(asset)
	return asset, nil
}
func (a *Audio) Read(asset string, wav bool) ([]byte, string, error) {
	a.Store.mu.Lock()
	var path, format string
	e := a.Store.DB.QueryRow("SELECT path,format FROM audio_assets WHERE id=?", asset).Scan(&path, &format)
	var raw []byte
	if e == nil {
		raw, e = os.ReadFile(filepath.Join(a.Store.Root, "audio", path))
	}
	a.Store.mu.Unlock()
	if e != nil {
		return nil, "", ErrNotFound
	}
	if wav && format == "flac" {
		raw, e = decodeFLAC(raw)
		format = "wav"
	}
	return raw, format, e
}
func (a *Audio) compressAsset(asset string) error {
	raw, format, e := a.Read(asset, false)
	if e != nil || format != "wav" {
		return e
	}
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	encoded, e := ffmpeg(ctx, a.Config.FFmpeg, raw, "-i", "pipe:0", "-c:a", "flac", "-compression_level", "5", "-threads", "1", "-f", "flac", "pipe:1")
	if e != nil {
		return e
	}
	decoded, e := decodeFLAC(encoded)
	if e != nil {
		return e
	}
	source, e := normalizeWAV(raw)
	if e != nil || !bytes.Equal(source, decoded) {
		return fmt.Errorf("无损校验失败")
	}
	temp := filepath.Join(a.Store.Root, "audio", asset+".flac.tmp")
	if e = os.WriteFile(temp, encoded, 0600); e != nil {
		return e
	}
	defer os.Remove(temp)
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	var old string
	if e = a.Store.DB.QueryRow("SELECT path FROM audio_assets WHERE id=? AND format='wav'", asset).Scan(&old); e != nil {
		return nil
	}
	name := asset + ".flac"
	if e = os.Rename(temp, filepath.Join(a.Store.Root, "audio", name)); e != nil {
		return e
	}
	if _, e = a.Store.DB.Exec("UPDATE audio_assets SET path=?,format='flac' WHERE id=?", name, asset); e != nil {
		return e
	}
	return os.Remove(filepath.Join(a.Store.Root, "audio", old))
}
func (a *Audio) Cleanup() (int, error) {
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	var active int
	if e := a.Store.DB.QueryRow("SELECT count(*) FROM jobs WHERE status IN ('queued','running')").Scan(&active); e != nil {
		return 0, e
	}
	if active > 0 {
		return 0, fmt.Errorf("请等待生成任务完成后清理缓存")
	}
	rows, e := a.Store.DB.Query("SELECT id,path FROM audio_assets WHERE id NOT IN (SELECT asset_id FROM audio_clips WHERE asset_id IS NOT NULL)")
	if e != nil {
		return 0, e
	}
	type entry struct{ id, path string }
	entries := []entry{}
	for rows.Next() {
		var v entry
		if e = rows.Scan(&v.id, &v.path); e != nil {
			rows.Close()
			return 0, e
		}
		entries = append(entries, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	for _, v := range entries {
		if _, e = a.Store.DB.Exec("DELETE FROM audio_assets WHERE id=?", v.id); e != nil {
			return 0, e
		}
		_ = os.Remove(filepath.Join(a.Store.Root, "audio", v.path))
	}
	// Recover files left by a crash between rename and DB commit. A grace period
	// protects an in-flight compressor's temporary file; published paths are kept.
	known := map[string]bool{}
	rows, e = a.Store.DB.Query("SELECT path FROM audio_assets")
	if e != nil {
		return len(entries), e
	}
	for rows.Next() {
		var path string
		if e = rows.Scan(&path); e != nil {
			rows.Close()
			return len(entries), e
		}
		known[path] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return len(entries), e
	}
	files, e := os.ReadDir(filepath.Join(a.Store.Root, "audio"))
	if e != nil {
		return len(entries), e
	}
	for _, file := range files {
		if file.IsDir() || known[file.Name()] {
			continue
		}
		ext := filepath.Ext(file.Name())
		if ext != ".wav" && ext != ".flac" && ext != ".tmp" {
			continue
		}
		info, err := file.Info()
		if err == nil && time.Since(info.ModTime()) > time.Hour {
			if err = os.Remove(filepath.Join(a.Store.Root, "audio", file.Name())); err != nil {
				return len(entries), err
			}
		}
	}
	return len(entries), nil
}
