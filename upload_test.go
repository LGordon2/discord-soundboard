package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
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

func TestUploadPassword(t *testing.T) {
	t.Setenv("UPLOAD_PASSWORD", "")
	filename := filepath.Join(t.TempDir(), "password")
	first, err := loadUploadPassword(filename)
	if err != nil || len(first) < 24 {
		t.Fatal("password generation failed")
	}
	second, err := loadUploadPassword(filename)
	if err != nil || second != first {
		t.Fatal("password not reused")
	}
	info, _ := os.Stat(filename)
	if info.Mode().Perm() != 0600 {
		t.Fatal("password permissions")
	}
	t.Setenv("UPLOAD_PASSWORD", "configured password")
	configured, err := loadUploadPassword(filename)
	if err != nil || configured != "configured password" {
		t.Fatal("configured password ignored")
	}
}
func testUploadHandler(t *testing.T) *soundUploadHandler {
	t.Helper()
	store, err := newSoundMetadataStore(testDatabase(t, filepath.Join(t.TempDir(), "db.sqlite3")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return &soundUploadHandler{directory: t.TempDir(), password: "test-password", metadata: store, commitMu: &sync.Mutex{}, running: make(chan struct{}, 1), publish: func(string, CreateSoundboardSoundRequest) {}, convert: convertUpload}
}
func sendUpload(t *testing.T, h *soundUploadHandler, password, name, filename, emoji string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"name": name, "description": "A test source", "emoji": emoji} {
		writer.WriteField(key, value)
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	writer.Close()
	req := httptest.NewRequest("POST", "/upload-sound", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Upload-Password", password)
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
	result := sendUpload(t, h, "test-password", "Test sound", "input.wav", "🦆", wavClip(.1))
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
	if duplicate := sendUpload(t, h, "test-password", "Test sound", "input.wav", "", wavClip(.1)); duplicate.Code != 409 {
		t.Fatal("duplicate overwritten")
	}
	if published != 1 {
		t.Fatal("duplicate published")
	}
	for _, test := range []struct {
		name string
		data []byte
	}{{"Too long", wavClip(5.5)}, {"Fake audio", []byte("this is not audio")}} {
		invalid := sendUpload(t, h, "test-password", test.name, "input.wav", "", test.data)
		if invalid.Code != 400 {
			t.Fatalf("invalid audio accepted: %s %d", test.name, invalid.Code)
		}
		if _, err := os.Stat(filepath.Join(h.directory, test.name+".mp3")); !os.IsNotExist(err) {
			t.Fatal("invalid audio saved")
		}
	}
	// A five-second source must remain under Discord's duration after MP3 padding.
	h.publish = func(string, CreateSoundboardSoundRequest) {}
	if edge := sendUpload(t, h, "test-password", "Five seconds", "input.wav", "", wavClip(5)); edge.Code != 201 {
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
	for _, test := range []struct{ ext, codec string }{{".mp3", "libmp3lame"}, {".ogg", "libvorbis"}} {
		t.Run(test.ext, func(t *testing.T) {
			encoded := filepath.Join(t.TempDir(), "audio"+test.ext)
			if err := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-i", input, "-c:a", test.codec, encoded).Run(); err != nil {
				t.Skip("test encoder unavailable")
			}
			data, err := os.ReadFile(encoded)
			if err != nil {
				t.Fatal(err)
			}
			response := sendUpload(t, h, "test-password", "Input "+test.codec, "original"+test.ext, "", data)
			if response.Code != 201 {
				t.Fatalf("%s input rejected: %s", test.ext, response.Body.String())
			}
		})
	}
}

func TestUploadValidationAndStorageFailure(t *testing.T) {
	h := testUploadHandler(t)
	converted := 0
	h.convert = func(_ context.Context, _ string, output string) error {
		converted++
		return os.WriteFile(output, []byte("encoded audio"), 0600)
	}
	for _, test := range []struct {
		password, name, file, emoji string
		status                      int
	}{
		{"wrong", "Name", "test.wav", "", 401},
		{"test-password", "../escape", "test.wav", "", 400},
		{"test-password", "x", "test.wav", "", 400},
		{"test-password", "Name", "test.txt", "", 400},
		{"test-password", "Name", "test.wav", "invalid emoji", 400},
	} {
		result := sendUpload(t, h, test.password, test.name, test.file, test.emoji, []byte("input"))
		if result.Code != test.status {
			t.Fatalf("validation %+v: %d", test, result.Code)
		}
	}
	if converted != 0 {
		t.Fatal("invalid requests reached converter")
	}
	if result := sendUpload(t, h, "test-password", "Oversized", "test.wav", "", make([]byte, uploadMaxBytes+1)); result.Code != 413 {
		t.Fatal("file size limit not enforced")
	}
	os.WriteFile(filepath.Join(h.directory, "Existing.ogg"), []byte("old sound"), 0600)
	if result := sendUpload(t, h, "test-password", "Existing", "test.wav", "", []byte("input")); result.Code != 409 {
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
	result := sendUpload(t, h, "test-password", "Database failed", "test.wav", "", []byte("input"))
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
	h.convert = func(context.Context, string, string) error {
		close(entered)
		<-release
		return fmt.Errorf("test conversion failed")
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- sendUpload(t, h, "test-password", "First", "test.wav", "", []byte("input")) }()
	<-entered
	second := sendUpload(t, h, "test-password", "Second", "test.wav", "", []byte("input"))
	close(release)
	if second.Code != 429 {
		t.Fatal("parallel conversion allowed")
	}
	if first := <-done; first.Code != 400 {
		t.Fatal("conversion failure not returned")
	}
}
