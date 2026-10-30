package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func wavClip(seconds float64) []byte {
	samples := int(44100 * seconds)
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+samples*2))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint32(44100))
	binary.Write(&b, binary.LittleEndian, uint32(88200))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(samples*2))
	b.Write(make([]byte, samples*2))
	return b.Bytes()
}

func testUploadHandler(t *testing.T) *soundUploadHandler {
	t.Helper()
	store, err := newSoundMetadataStore(testDatabase(t, filepath.Join(t.TempDir(), "db.sqlite3")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return &soundUploadHandler{directory: t.TempDir(), authorize: func(r *http.Request) (uploadIdentity, int) {
		if r.Header.Get("X-Test-User") != "test-user" {
			return uploadIdentity{}, 401
		}
		return uploadIdentity{ID: "12345", Username: "uploader"}, 0
	}, metadata: store, commitMu: &sync.Mutex{}, running: make(chan struct{}, 1), publish: func(string, CreateSoundboardSoundRequest) {}, convert: convertUploadEdit}
}
func sendUpload(t *testing.T, h *soundUploadHandler, identity, name, filename, emoji string, data []byte, extra ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"name": name, "description": "A test source", "emoji": emoji} {
		writer.WriteField(key, value)
	}
	for _, fields := range extra {
		for key, value := range fields {
			writer.WriteField(key, value)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	writer.Close()
	req := httptest.NewRequest("POST", "/upload-sound", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Test-User", identity)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	return response
}
func TestUploadRealAudioAndMetadata(t *testing.T) {
	for _, program := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skip(program + " unavailable")
		}
	}
	h := testUploadHandler(t)
	published := 0
	h.publish = func(filename string, request CreateSoundboardSoundRequest) {
		published++
		if filename != "Test sound.mp3" || request.Name != "Test sound" || !strings.HasPrefix(request.Sound, "data:audio/mpeg;base64,") {
			t.Fatal("wrong upload cache entry")
		}
	}
	result := sendUpload(t, h, "test-user", "Test sound", "input.wav", "🦆", wavClip(.1))
	if result.Code != 201 {
		t.Fatalf("upload: %d %s", result.Code, result.Body.String())
	}
	var value map[string]string
	if json.Unmarshal(result.Body.Bytes(), &value) != nil || value["filename"] != "Test sound.mp3" {
		t.Fatal("wrong response")
	}
	if published != 1 {
		t.Fatal("upload not published")
	}
	if err := probeUpload(context.Background(), filepath.Join(h.directory, "Test sound.mp3"), 5.2); err != nil {
		t.Fatal(err)
	}
	entry := h.metadata.Snapshot()["Test sound"]
	if entry.Description != "A test source" || entry.EmojiName != "🦆" {
		t.Fatal("metadata lost")
	}
	files, _ := os.ReadDir(h.directory)
	if len(files) != 1 {
		t.Fatal("temporary files not cleaned")
	}
	if duplicate := sendUpload(t, h, "test-user", "Test sound", "input.wav", "", wavClip(.1)); duplicate.Code != 409 {
		t.Fatal("duplicate overwritten")
	}
	if published != 1 {
		t.Fatal("duplicate published")
	}
	for _, test := range []struct {
		name string
		data []byte
	}{{"Too long", wavClip(5.5)}, {"Fake audio", []byte("this is not audio")}} {
		invalid := sendUpload(t, h, "test-user", test.name, "input.wav", "", test.data)
		if invalid.Code != 400 {
			t.Fatalf("invalid audio accepted: %s %d", test.name, invalid.Code)
		}
		if _, err := os.Stat(filepath.Join(h.directory, test.name+".mp3")); !os.IsNotExist(err) {
			t.Fatal("invalid audio saved")
		}
	}
	// A five-second source must remain under Discord's duration after MP3 padding.
	h.publish = func(string, CreateSoundboardSoundRequest) {}
	if edge := sendUpload(t, h, "test-user", "Five seconds", "input.wav", "", wavClip(5)); edge.Code != 201 {
		t.Fatalf("five-second clip rejected: %s", edge.Body.String())
	}
}
func TestUploadMP3AndOggInputs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	h := testUploadHandler(t)
	input := filepath.Join(t.TempDir(), "source.wav")
	if err := os.WriteFile(input, wavClip(.1), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ ext, codec string }{{".mp3", "libmp3lame"}, {".ogg", "libvorbis"}, {".oga", "libvorbis"}, {".OGA", "libvorbis"}} {
		t.Run(test.ext, func(t *testing.T) {
			encoded := filepath.Join(t.TempDir(), "audio"+test.ext)
			if err := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-i", input, "-c:a", test.codec, encoded).Run(); err != nil {
				t.Skip("test encoder unavailable")
			}
			data, err := os.ReadFile(encoded)
			if err != nil {
				t.Fatal(err)
			}
			response := sendUpload(t, h, "test-user", "Input "+test.ext[1:], "original"+test.ext, "", data)
			if response.Code != 201 {
				t.Fatalf("%s input rejected: %s", test.ext, response.Body.String())
			}
		})
	}
}

func TestUploadOggCoverArtAndVideo(t *testing.T) {
	for _, program := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skip(program + " unavailable")
		}
	}
	dir := t.TempDir()
	cover := filepath.Join(dir, "cover.jpg")
	if data, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=size=16x16", "-frames:v", "1", cover).CombinedOutput(); err != nil {
		t.Fatalf("generate cover: %v: %s", err, data)
	}
	image, err := os.ReadFile(cover)
	if err != nil {
		t.Fatal(err)
	}
	// Ogg stores cover art as a FLAC picture block in a Vorbis comment.
	var picture bytes.Buffer
	for _, value := range []uint32{3, 10} {
		binary.Write(&picture, binary.BigEndian, value)
	}
	picture.WriteString("image/jpeg")
	for _, value := range []uint32{0, 16, 16, 24, 0, uint32(len(image))} {
		binary.Write(&picture, binary.BigEndian, value)
	}
	picture.Write(image)
	h := testUploadHandler(t)
	for _, test := range []struct {
		name   string
		args   []string
		status int
	}{
		{"Cover art", []string{"-c:a", "libvorbis", "-metadata", "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(picture.Bytes())}, 201},
		{"Video", []string{"-f", "lavfi", "-i", "color=size=64x64:duration=0.1", "-c:a", "libvorbis", "-c:v", "libtheora", "-shortest"}, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(dir, test.name+".oga")
			args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.1"}
			args = append(args, test.args...)
			args = append(args, "-f", "ogg", filename)
			if data, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("generate Ogg: %v: %s", err, data)
			}
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			response := sendUpload(t, h, "test-user", test.name, "original.oga", "", data)
			if response.Code != test.status {
				t.Fatalf("status %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			if test.status == 400 && !strings.Contains(response.Body.String(), "audio-only") {
				t.Fatalf("expected video rejection: %s", response.Body.String())
			}
		})
	}
}

func TestUploadValidationAndStorageFailure(t *testing.T) {
	h := testUploadHandler(t)
	converted := 0
	h.convert = func(_ context.Context, _ string, output string, _ uploadEdit) error {
		converted++
		return os.WriteFile(output, []byte("encoded audio"), 0600)
	}
	for _, test := range []struct {
		identity, name, file, emoji string
		status                      int
	}{
		{"wrong", "Name", "test.wav", "", 401},
		{"test-user", "../escape", "test.wav", "", 400},
		{"test-user", "x", "test.wav", "", 400},
		{"test-user", "Name", "test.txt", "", 400},
		{"test-user", "Name", "test.wav", "invalid emoji", 400},
	} {
		result := sendUpload(t, h, test.identity, test.name, test.file, test.emoji, []byte("input"))
		if result.Code != test.status {
			t.Fatalf("validation %+v: %d", test, result.Code)
		}
	}
	if converted != 0 {
		t.Fatal("invalid requests reached converter")
	}
	if result := sendUpload(t, h, "test-user", "Oversized", "test.wav", "", make([]byte, uploadMaxBytes+1)); result.Code != 413 {
		t.Fatal("file size limit not enforced")
	}
	os.WriteFile(filepath.Join(h.directory, "Existing.ogg"), []byte("old sound"), 0600)
	if result := sendUpload(t, h, "test-user", "Existing", "test.wav", "", []byte("input")); result.Code != 409 {
		t.Fatal("Ogg variant not protected")
	}
	if converted != 0 {
		t.Fatal("duplicate unnecessarily converted")
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest("GET", "/upload-sound", nil))
	if response.Code != 405 {
		t.Fatal("wrong method allowed")
	}
	h.metadata.db.Close()
	result := sendUpload(t, h, "test-user", "Database failed", "test.wav", "", []byte("input"))
	if result.Code != 500 {
		t.Fatalf("database failure: %d", result.Code)
	}
	if _, err := os.Stat(filepath.Join(h.directory, "Database failed.mp3")); !os.IsNotExist(err) {
		t.Fatal("failed metadata save left audio behind")
	}
	if _, ok := h.metadata.Snapshot()["Database failed"]; ok {
		t.Fatal("failed metadata save changed memory")
	}
}
func TestUploadConcurrencyLimit(t *testing.T) {
	h := testUploadHandler(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.convert = func(context.Context, string, string, uploadEdit) error {
		close(entered)
		<-release
		return fmt.Errorf("test conversion failed")
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- sendUpload(t, h, "test-user", "First", "test.wav", "", []byte("input")) }()
	<-entered
	second := sendUpload(t, h, "test-user", "Second", "test.wav", "", []byte("input"))
	close(release)
	if second.Code != 429 {
		t.Fatal("parallel conversion allowed")
	}
	if first := <-done; first.Code != 400 {
		t.Fatal("conversion failure not returned")
	}
}

func TestUploadTrimAndVolume(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	input := filepath.Join(t.TempDir(), "long.wav")
	data := wavClip(8)
	for sample := 3 * 44100; sample < 4*44100; sample++ {
		value := int16(math.Sin(2*math.Pi*440*float64(sample)/44100) * 16383)
		binary.LittleEndian.PutUint16(data[44+sample*2:], uint16(value))
	}
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	var levels []float64
	for _, volume := range []float64{1, .25} {
		output := filepath.Join(t.TempDir(), "trim.mp3")
		if err := convertUploadEdit(context.Background(), input, output, uploadEdit{Start: 3, End: 4, Volume: volume, Trim: true}); err != nil {
			t.Fatal(err)
		}
		duration, err := uploadAudioDuration(context.Background(), output, 5.2)
		if err != nil || duration > 1.2 || duration < 1 {
			t.Fatalf("wrong clip duration: %f %v", duration, err)
		}
		decoded, err := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-i", output, "-f", "f32le", "-ar", "8000", "-ac", "1", "pipe:1").Output()
		if err != nil {
			t.Fatal(err)
		}
		sum := 0.0
		for i := 0; i+4 <= len(decoded); i += 4 {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(decoded[i:])))
			sum += value * value
		}
		levels = append(levels, math.Sqrt(sum/float64(len(decoded)/4)))
	}
	if levels[0] < .1 {
		t.Fatal("trim selected silence instead of the requested tone")
	}
	ratio := levels[1] / levels[0]
	if ratio < .20 || ratio > .30 {
		t.Fatalf("volume not applied: ratio=%f", ratio)
	}
	h := testUploadHandler(t)
	result := sendUpload(t, h, "test-user", "Trimmed upload", "long.wav", "🦆", data, map[string]string{"start": "3", "end": "4", "volume": "0.25"})
	if result.Code != 201 {
		t.Fatalf("edited upload rejected: %s", result.Body.String())
	}
	var uploader string
	if err := h.metadata.db.QueryRow("SELECT uploader_id FROM sound_uploads WHERE filename='Trimmed upload.mp3'").Scan(&uploader); err != nil || uploader != "12345" {
		t.Fatal("verified uploader attribution not stored")
	}
}
func TestUploadInvalidEditsAndRevokedAccess(t *testing.T) {
	h := testUploadHandler(t)
	converted := 0
	h.convert = func(_ context.Context, _ string, path string, _ uploadEdit) error {
		converted++
		return os.WriteFile(path, []byte("encoded"), 0600)
	}
	for _, fields := range []map[string]string{
		{"start": "NaN", "end": "4"}, {"start": "0", "end": "Inf"}, {"start": "-1", "end": "3"}, {"start": "2", "end": "1"}, {"start": "0", "end": "6"}, {"start": "1"}, {"volume": "NaN"}, {"volume": "2"},
	} {
		response := sendUpload(t, h, "test-user", "Invalid clip", "clip.wav", "", []byte("input"), fields)
		if response.Code != 400 {
			t.Fatalf("invalid edit accepted: %+v", fields)
		}
	}
	if converted != 0 {
		t.Fatal("invalid edits reached converter")
	}
	allowed := true
	h.authorize = func(*http.Request) (uploadIdentity, int) {
		if !allowed {
			return uploadIdentity{}, 403
		}
		return uploadIdentity{ID: "12345", Username: "approved"}, 0
	}
	h.convert = func(_ context.Context, _ string, path string, _ uploadEdit) error {
		allowed = false
		return os.WriteFile(path, []byte("encoded"), 0600)
	}
	response := sendUpload(t, h, "test-user", "Revoked clip", "clip.wav", "", []byte("input"))
	if response.Code != 403 {
		t.Fatal("access revoked during conversion still allowed")
	}
	if _, err := os.Stat(filepath.Join(h.directory, "Revoked clip.mp3")); !os.IsNotExist(err) {
		t.Fatal("revoked upload published")
	}
}

func TestUploadRejectsDisguisedNonAudio(t *testing.T) {
	for _, program := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skip(program + " unavailable")
		}
	}
	h := testUploadHandler(t)
	published := false
	h.publish = func(string, CreateSoundboardSoundRequest) { published = true }
	for _, ext := range []string{".mp3", ".ogg", ".oga", ".wav"} {
		for _, test := range []struct {
			name string
			data []byte
		}{
			{"Text", []byte("This is a document, not audio.")},
			{"HTML", []byte("<html><body>Not audio</body></html>")},
			{"Fake MP3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00not audio frames")},
			{"Fake Ogg", []byte("OggS\x00invalid container")},
			{"Truncated WAV", wavClip(.1)[:44]},
		} {
			t.Run(test.name+ext, func(t *testing.T) {
				response := sendUpload(t, h, "test-user", test.name, "disguised"+ext, "", test.data)
				if response.Code != 400 {
					t.Fatalf("disguised file accepted: %d %s", response.Code, response.Body.String())
				}
			})
		}
	}
	if published {
		t.Fatal("invalid audio was published")
	}
	files, err := os.ReadDir(h.directory)
	if err != nil || len(files) != 0 {
		t.Fatal("invalid uploads left files", err)
	}
}
