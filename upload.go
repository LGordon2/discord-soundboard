package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const uploadMaxBytes = 10 << 20

type soundLibrary struct {
	files   []string
	uploads storedSoundMap
}

func validUploadName(name string) bool {
	if utf8.RuneCountInString(name) < 2 || utf8.RuneCountInString(name) > 32 || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

type audioProbe struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
	} `json:"streams"`
}

type uploadEdit struct {
	Start, End, Volume float64
	Trim               bool
}

func parseUploadEdit(r *http.Request) (uploadEdit, error) {
	edit := uploadEdit{Volume: 1}
	start, end, volume := r.FormValue("start"), r.FormValue("end"), r.FormValue("volume")
	if volume != "" {
		value, err := strconv.ParseFloat(volume, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return edit, fmt.Errorf("volume must be between 0 and 100 percent")
		}
		edit.Volume = value
	}
	if start != "" || end != "" {
		var err error
		edit.Start, err = strconv.ParseFloat(start, 64)
		if err != nil {
			return edit, fmt.Errorf("choose a valid clip start")
		}
		edit.End, err = strconv.ParseFloat(end, 64)
		if err != nil {
			return edit, fmt.Errorf("choose a valid clip end")
		}
		if math.IsNaN(edit.Start) || math.IsNaN(edit.End) || math.IsInf(edit.Start, 0) || math.IsInf(edit.End, 0) || edit.Start < 0 || edit.End <= edit.Start || edit.End-edit.Start > 5.000001 || edit.End > 120 {
			return edit, fmt.Errorf("select a clip up to five seconds from audio up to two minutes")
		}
		edit.Trim = true
	}
	return edit, nil
}
func uploadAudioDuration(ctx context.Context, filename string, limit float64) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,ogg,wav", "-show_entries", "format=duration:stream=codec_type", "-of", "json", filename)
	data, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("could not read audio; choose a valid MP3, Ogg, or WAV file")
	}
	var probe audioProbe
	if json.Unmarshal(data, &probe) != nil {
		return 0, fmt.Errorf("could not read audio information")
	}
	seconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > limit {
		return 0, fmt.Errorf("audio is longer than the allowed duration")
	}
	audio := false
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			audio = true
		} else {
			return 0, fmt.Errorf("upload an audio-only file")
		}
	}
	if !audio {
		return 0, fmt.Errorf("file contains no audio")
	}
	return seconds, nil
}
func probeUpload(ctx context.Context, filename string, limit float64) error {
	_, err := uploadAudioDuration(ctx, filename, limit)
	return err
}
func convertUpload(ctx context.Context, input, output string) error {
	return convertUploadEdit(ctx, input, output, uploadEdit{Volume: 1})
}
func convertUploadEdit(ctx context.Context, input, output string, edit uploadEdit) error {
	limit := 5.0
	if edit.Trim {
		limit = 120
	}
	duration, err := uploadAudioDuration(ctx, input, limit)
	if err != nil {
		return err
	}
	start, length := 0.0, duration
	if edit.Trim {
		if edit.End > duration+.025 {
			return fmt.Errorf("selected clip ends after the audio")
		}
		start, length = edit.Start, math.Min(edit.End, duration)-edit.Start
	}
	if length <= 0 || length > 5.000001 {
		return fmt.Errorf("select a clip up to five seconds")
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,ogg,wav", "-ss", strconv.FormatFloat(start, 'f', 6, 64), "-i", input, "-map", "0:a:0", "-vn", "-sn", "-dn", "-map_metadata", "-1", "-t", strconv.FormatFloat(length, 'f', 6, 64), "-af", "volume=" + strconv.FormatFloat(edit.Volume, 'f', 6, 64), "-c:a", "libmp3lame", "-b:a", "128k", "-ar", "44100", "-ac", "2", "-f", "mp3", "-y", output}
	if err := exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
		return fmt.Errorf("audio conversion failed; try another file")
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 || info.Size() > 512*1024 {
		return fmt.Errorf("converted clip exceeds Discord's audio size limit")
	}
	return probeUpload(ctx, output, 5.2)
}

type soundUploadHandler struct {
	directory string
	authorize func(*http.Request) (uploadIdentity, int)
	metadata  *SoundMetadataStore
	commitMu  *sync.Mutex
	running   chan struct{}
	publish   func(string, CreateSoundboardSoundRequest)
	convert   func(context.Context, string, string, uploadEdit) error
}

func (h *soundUploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Use POST", 405)
		return
	}
	user, status := h.authorize(r)
	if status != 0 {
		message := "Sign in with Discord to upload sounds"
		if status == 403 {
			message = "This Discord account is not allowed to upload, or the session needs refreshing"
		}
		if status == 503 {
			message = "Discord upload login is not configured"
		}
		http.Error(w, message, status)
		return
	}

	select {
	case h.running <- struct{}{}:
		defer func() { <-h.running }()
	default:
		http.Error(w, "Another upload is processing. Try again shortly.", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, uploadMaxBytes+65536)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		http.Error(w, "Choose a file under 10 MB", 413)
		return
	}
	defer r.MultipartForm.RemoveAll()
	name, description := strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("description"))
	if !validUploadName(name) || utf8.RuneCountInString(description) > 2000 {
		http.Error(w, "Use a name of 2–32 characters without path separators, and a description under 2000 characters", 400)
		return
	}
	for _, ext := range []string{".mp3", ".ogg"} {
		if _, err := os.Lstat(filepath.Join(h.directory, name+ext)); err == nil {
			http.Error(w, "A sound with this name already exists. Choose another name.", 409)
			return
		}
	}
	id, emoji, err := parseEditedEmoji(r.FormValue("emoji"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	edit, err := parseUploadEdit(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	source, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Choose an audio file", 400)
		return
	}
	defer source.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".mp3" && ext != ".ogg" && ext != ".wav" {
		http.Error(w, "Choose MP3, Ogg, or WAV audio", 400)
		return
	}
	if header.Size <= 0 || header.Size > uploadMaxBytes {
		http.Error(w, "Choose a non-empty file under 10 MB", 413)
		return
	}
	temp, err := os.MkdirTemp(h.directory, ".upload-")
	if err != nil {
		http.Error(w, "Could not prepare upload storage", 500)
		return
	}
	defer os.RemoveAll(temp)
	input, output := filepath.Join(temp, "source"+ext), filepath.Join(temp, "converted.mp3")
	file, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		http.Error(w, "Could not prepare audio", 500)
		return
	}
	_, copyErr := io.Copy(file, source)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		http.Error(w, "Could not read uploaded audio", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := h.convert(ctx, input, output, edit); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	data, err := os.ReadFile(output)
	if err != nil || len(data) == 0 || len(data) > 512*1024 {
		http.Error(w, "Could not prepare converted audio", 500)
		return
	}
	if _, status := h.authorize(r); status != 0 {
		http.Error(w, "Your upload access changed. Sign in again or contact the site owner.", status)
		return
	}
	prepared := prepareSoundboardSound(name, "audio/mpeg", data)
	h.commitMu.Lock()
	defer h.commitMu.Unlock()
	for _, variant := range []string{".mp3", ".ogg"} {
		if _, err := os.Lstat(filepath.Join(h.directory, name+variant)); err == nil {
			http.Error(w, "A sound with this name already exists. Choose another name.", 409)
			return
		} else if !os.IsNotExist(err) {
			http.Error(w, "Could not check sound storage", 500)
			return
		}
	}
	target := filepath.Join(h.directory, name+".mp3")
	if err := os.Chmod(output, 0644); err != nil {
		http.Error(w, "Could not save audio", 500)
		return
	}
	// Same-filesystem hard linking publishes the finished file without overwriting.
	if err := os.Link(output, target); err != nil {
		http.Error(w, "Could not save sound; the name may already exist", 409)
		return
	}
	if err := h.metadata.saveUploadedDetails(name, description, id, emoji, user); err != nil {
		os.Remove(target)
		http.Error(w, "Could not save sound details", 500)
		return
	}
	h.publish(name+".mp3", prepared)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]string{"name": name, "filename": name + ".mp3"})
}
