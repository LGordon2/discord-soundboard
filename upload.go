package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
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

func loadUploadPassword(filename string) (string, error) {
	if configured := os.Getenv("UPLOAD_PASSWORD"); configured != "" {
		return configured, nil
	}
	if data, err := os.ReadFile(filename); err == nil {
		if password := strings.TrimSpace(string(data)); len(password) >= 16 {
			return password, nil
		}
		return "", fmt.Errorf("upload password file must contain at least 16 characters")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return "", err
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	password := base64.RawURLEncoding.EncodeToString(random[:])
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = file.WriteString(password + "\n")
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	return password, nil
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

func probeUpload(ctx context.Context, filename string, limit float64) error {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,ogg,wav", "-show_entries", "format=duration:stream=codec_type", "-of", "json", filename)
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("could not read audio; choose a valid MP3, Ogg, or WAV file")
	}
	var probe audioProbe
	if json.Unmarshal(data, &probe) != nil {
		return fmt.Errorf("could not read audio information")
	}
	seconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > limit {
		return fmt.Errorf("choose a clip no longer than five seconds")
	}
	audio := false
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			audio = true
		} else {
			return fmt.Errorf("upload an audio-only file")
		}
	}
	if !audio {
		return fmt.Errorf("file contains no audio")
	}
	return nil
}
func convertUpload(ctx context.Context, input, output string) error {
	if err := probeUpload(ctx, input, 5.0); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,ogg,wav", "-i", input, "-map", "0:a:0", "-vn", "-sn", "-dn", "-map_metadata", "-1", "-c:a", "libmp3lame", "-b:a", "128k", "-ar", "44100", "-ac", "2", "-t", "5.1", "-f", "mp3", "-y", output)
	if err := cmd.Run(); err != nil {
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
	password  string
	metadata  *SoundMetadataStore
	commitMu  *sync.Mutex
	running   chan struct{}
	publish   func(string, CreateSoundboardSoundRequest)
	convert   func(context.Context, string, string) error
}

func (h *soundUploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Use POST", 405)
		return
	}
	if h.password == "" {
		http.Error(w, "Uploads are unavailable", 503)
		return
	}
	expected, provided := sha256.Sum256([]byte(h.password)), sha256.Sum256([]byte(r.Header.Get("X-Upload-Password")))
	if subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
		http.Error(w, "Incorrect upload password", 401)
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
	if err := h.convert(ctx, input, output); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	data, err := os.ReadFile(output)
	if err != nil || len(data) == 0 || len(data) > 512*1024 {
		http.Error(w, "Could not prepare converted audio", 500)
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
	if err := h.metadata.saveUploadedDetails(name, description, id, emoji); err != nil {
		os.Remove(target)
		http.Error(w, "Could not save sound details", 500)
		return
	}
	h.publish(name+".mp3", prepared)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]string{"name": name, "filename": name + ".mp3"})
}
