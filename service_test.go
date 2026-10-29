package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testDiscordClient(t *testing.T, fn roundTripFunc) *DiscordRestClient {
	t.Helper()
	session, err := discordgo.New("test-token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client = &http.Client{Transport: fn}
	return &DiscordRestClient{discord: session}
}

func TestSwapSoundOrderAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		input        swapSoundInput
		deleteStatus int
		want         []string
		wantErr      bool
	}{
		{"replacement", swapSoundInput{Add: addSoundInput{"new.ogg"}, Delete: deleteSoundInput{"old"}}, 204, []string{"DELETE", "POST"}, false},
		{"failed delete", swapSoundInput{Add: addSoundInput{"new.ogg"}, Delete: deleteSoundInput{"old"}}, 403, []string{"DELETE"}, true},
		{"invalid replacement", swapSoundInput{Add: addSoundInput{"../missing.ogg"}, Delete: deleteSoundInput{"old"}}, 204, nil, true},
		{"add only", swapSoundInput{Add: addSoundInput{"new.ogg"}}, 204, []string{"POST"}, false},
		{"delete only", swapSoundInput{Delete: deleteSoundInput{"old"}}, 204, []string{"DELETE"}, false},
		{"empty", swapSoundInput{}, 204, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var methods []string
			client := testDiscordClient(t, func(r *http.Request) (*http.Response, error) {
				methods = append(methods, r.Method)
				status, body := tc.deleteStatus, `{}`
				if r.Method == "POST" {
					status, body = 201, `{"sound_id":"new-id","name":"new"}`
					var request CreateSoundboardSoundRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					if request.Sound != "data:audio/ogg;base64,YXVkaW8=" || request.Name != "new" || request.Volume != 1 {
						t.Fatalf("incorrect payload: %+v", request)
					}
				}
				if r.Header.Get("X-Super-Properties") == "" {
					t.Error("missing existing Discord header")
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			stored := storedSoundMap{"new.ogg": prepareSoundboardSound("new", "audio/ogg", []byte("audio"))}
			err := swapSound(client, stored, tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if !reflect.DeepEqual(methods, tc.want) {
				t.Fatalf("requests=%v want %v", methods, tc.want)
			}
		})
	}
}

func TestStoredUploadsAndDiskFallback(t *testing.T) {
	original := soundsDir
	soundsDir = t.TempDir()
	defer func() { soundsDir = original }()
	for filename, data := range map[string]string{"same.ogg": "ogg", "same.mp3": "mp3", "empty.ogg": ""} {
		if err := os.WriteFile(filepath.Join(soundsDir, filename), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, stored, err := fetchStoredSounds()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || !stored.containsName("same") {
		t.Fatalf("incorrect cache: %v", stored)
	}
	// Cached uploads remain usable without reading audio from disk again.
	if err := os.Remove(filepath.Join(soundsDir, "same.ogg")); err != nil {
		t.Fatal(err)
	}
	ogg, err := prepareStoredSound(stored, addSoundInput{"same.ogg"})
	if err != nil || ogg.Sound != "data:audio/ogg;base64,b2dn" {
		t.Fatalf("ogg=%+v err=%v", ogg, err)
	}
	mp3, err := prepareStoredSound(nil, addSoundInput{"same.mp3"})
	if err != nil || mp3.Sound != "data:audio/mpeg;base64,bXAz" {
		t.Fatalf("mp3=%+v err=%v", mp3, err)
	}
	for _, filename := range []string{"empty.ogg", "missing.ogg", "../same.mp3", "same.wav"} {
		if _, err := prepareStoredSound(stored, addSoundInput{filename}); err == nil {
			t.Errorf("accepted %s", filename)
		}
	}
}

func TestCreateStillHonorsRateLimitRetry(t *testing.T) {
	calls := 0
	start := time.Now()
	client := testDiscordClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		status, body := 201, `{"sound_id":"new-id"}`
		if calls == 1 {
			status, body = 429, `{"retry_after":0.02,"global":false}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result, err := client.createSoundboardSound(guildID, prepareSoundboardSound("new", "audio/ogg", []byte("audio")))
	if err != nil || result.SoundID != "new-id" || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Fatal("retry did not wait")
	}
}

func TestCreatePropagatesTransportFailure(t *testing.T) {
	want := errors.New("network failure")
	client := testDiscordClient(t, func(*http.Request) (*http.Response, error) { return nil, want })
	_, err := client.CreateSoundboardSound(guildID, "new", "audio/ogg", []byte("audio"))
	if !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}

func BenchmarkPrepareStoredSound(b *testing.B) {
	data := make([]byte, 256*1024)
	stored := storedSoundMap{"new.ogg": prepareSoundboardSound("new", "audio/ogg", data)}
	b.Run("encode_each_switch", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkUpload = prepareSoundboardSound("new", "audio/ogg", data)
		}
	})
	b.Run("cached_upload", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkUpload, _ = prepareStoredSound(stored, addSoundInput{"new.ogg"})
		}
	})
}

var benchmarkUpload CreateSoundboardSoundRequest
