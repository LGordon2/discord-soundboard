package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSwitchTimingsCorrelateStagesAndConfirmation(t *testing.T) {
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(original)
	state := newSharedSwitches(time.Second, func([]byte) {})
	token, err := state.Begin(2, "new")
	if err != nil {
		t.Fatal(err)
	}
	operation := fmt.Sprintf("%s-%d", state.epoch, token)
	client := testDiscordClient(t, func(r *http.Request) (*http.Response, error) {
		status, body := 204, ""
		if r.Method == "POST" {
			status, body = 201, `{"sound_id":"created"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result, err := swapSoundResult(client, storedSoundMap{"new.ogg": prepareSoundboardSound("new", "audio/ogg", []byte("private audio"))}, swapSoundInput{trace: operation, Add: addSoundInput{"new.ogg"}, Delete: deleteSoundInput{"old"}})
	if err != nil {
		t.Fatal(err)
	}
	state.Accepted(2, token, result.SoundID)
	state.Resolve([]SoundboardSoundWithOrdinal{{SoundboardSound: SoundboardSound{ID: "unrelated"}}})
	if strings.Contains(logs.String(), "outcome=confirmed") {
		t.Fatal("unrelated sound completed timing")
	}
	state.Resolve([]SoundboardSoundWithOrdinal{{SoundboardSound: SoundboardSound{ID: "created"}}})
	state.Resolve([]SoundboardSoundWithOrdinal{{SoundboardSound: SoundboardSound{ID: "created"}}})
	for _, stage := range []string{"prepare", "delete", "create", "accepted", "complete"} {
		if !strings.Contains(logs.String(), "operation="+operation+" ") || !strings.Contains(logs.String(), "stage="+stage+" ") {
			t.Fatalf("missing stage %s: %s", stage, &logs)
		}
	}
	if strings.Count(logs.String(), "outcome=confirmed") != 1 || !strings.Contains(logs.String(), "gateway_wait_ms=") || !strings.Contains(logs.String(), "total_ms=") {
		t.Fatalf("incorrect completion: %s", &logs)
	}
	if strings.Contains(logs.String(), "private audio") || strings.Contains(logs.String(), "test-token") {
		t.Fatal("sensitive data logged")
	}
}
