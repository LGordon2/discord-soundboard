package main

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func decodeSharedState(t *testing.T, frame []byte) SharedSwitchSnapshot {
	t.Helper()
	var event liveEvent
	if err := json.Unmarshal(frame, &event); err != nil {
		t.Fatal(err)
	}
	if event.Switches == nil {
		t.Fatal("missing switch state")
	}
	snapshot := *event.Switches
	return snapshot
}

func TestSharedLoadingBroadcastConflictAndLateJoin(t *testing.T) {
	first, second := make(chan []byte, 10), make(chan []byte, 10)
	state := newSharedSwitches(time.Second, func(frame []byte) { first <- frame; second <- frame })
	var board [soundboardSoundCount]SoundboardSound
	board[0] = SoundboardSound{ID: "old", Name: "old"}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	handler := &trackedSwapHandler{state: state, board: func() [soundboardSoundCount]SoundboardSound { return board }, execute: func(swapSoundInput) (CreateSoundboardSoundResponse, error) {
		calls.Add(1)
		close(entered)
		<-release
		return CreateSoundboardSoundResponse{SoundID: "created"}, nil
	}, refresh: func([soundboardSoundCount]SoundboardSound) { t.Error("ID has not reached the board yet") }}
	result := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(result, httptest.NewRequest("POST", "/swap-sound", strings.NewReader(`{"slot":0,"add":{"soundLocation":"new.ogg"},"delete":{"soundID":"old"}}`)))
		close(done)
	}()
	<-entered
	for _, viewer := range []chan []byte{first, second} {
		snapshot := decodeSharedState(t, <-viewer)
		if len(snapshot.Pending) != 1 || snapshot.Pending[0].Slot != 0 || snapshot.Pending[0].Name != "new" {
			t.Fatalf("wrong observer state: %+v", snapshot)
		}
	}
	late := decodeSharedState(t, state.Frame())
	if len(late.Pending) != 1 {
		t.Fatal("late join missed active upload")
	}
	for _, body := range []string{
		`{"slot":0,"add":{"soundLocation":"other.ogg"},"delete":{"soundID":"old"}}`,
		`{"slot":1,"add":{"soundLocation":"new.ogg"}}`,
	} {
		conflict := httptest.NewRecorder()
		handler.ServeHTTP(conflict, httptest.NewRequest("POST", "/swap-sound", strings.NewReader(body)))
		if conflict.Code != 409 {
			t.Fatalf("conflict status=%d", conflict.Code)
		}
	}
	close(release)
	<-done
	if result.Code != 200 || calls.Load() != 1 {
		t.Fatalf("status=%d calls=%d", result.Code, calls.Load())
	}
	if !state.Busy(0) {
		t.Fatal("HTTP acceptance cleared loading before Gateway confirmation")
	}
	state.Resolve([]SoundboardSoundWithOrdinal{{SoundboardSound: SoundboardSound{ID: "different", Name: "new"}, ordinal: 0}})
	if !state.Busy(0) {
		t.Fatal("unrelated ID completed an upload")
	}
	state.Resolve([]SoundboardSoundWithOrdinal{{SoundboardSound: SoundboardSound{ID: "created", Name: "new"}, ordinal: 2}})
	completed := decodeSharedState(t, state.Frame())
	if len(completed.Pending) != 0 || completed.Version <= late.Version {
		t.Fatalf("loading not completed: %+v", completed)
	}
}

func TestSharedLoadingFailureTimeoutAndEarlyGateway(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		frames := make(chan []byte, 4)
		state := newSharedSwitches(time.Second, func(frame []byte) { frames <- frame })
		handler := &trackedSwapHandler{state: state, board: func() [soundboardSoundCount]SoundboardSound { return [soundboardSoundCount]SoundboardSound{} }, execute: func(swapSoundInput) (CreateSoundboardSoundResponse, error) {
			return CreateSoundboardSoundResponse{}, errors.New("Discord rejected upload")
		}}
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest("POST", "/swap-sound", strings.NewReader(`{"slot":1,"add":{"soundLocation":"failed.ogg"}}`)))
		start, end := decodeSharedState(t, <-frames), decodeSharedState(t, <-frames)
		if result.Code != 500 || len(start.Pending) != 1 || len(end.Pending) != 0 || state.Busy(1) {
			t.Fatalf("failed upload stuck: %d %+v", result.Code, end)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		frames := make(chan []byte, 4)
		state := newSharedSwitches(5*time.Millisecond, func(frame []byte) { frames <- frame })
		token, err := state.Begin(0, `quoted "name" <script>`)
		if err != nil {
			t.Fatal(err)
		}
		frame := <-frames
		if strings.Contains(string(frame), "<script>") {
			t.Fatal("unescaped state HTML")
		}
		state.Accepted(0, token, "never-confirmed")
		select {
		case frame := <-frames:
			if len(decodeSharedState(t, frame).Pending) != 0 {
				t.Fatal("timeout did not clear loading")
			}
		case <-time.After(time.Second):
			t.Fatal("loading timeout did not fire")
		}
		state.Remove(0, token)
		select {
		case <-frames:
			t.Fatal("duplicate completion published")
		default:
		}
	})
	t.Run("gateway before HTTP", func(t *testing.T) {
		state := newSharedSwitches(time.Second, func([]byte) {})
		var board [soundboardSoundCount]SoundboardSound
		refreshed := false
		handler := &trackedSwapHandler{state: state, board: func() [soundboardSoundCount]SoundboardSound { return board }, execute: func(swapSoundInput) (CreateSoundboardSoundResponse, error) {
			board[0] = SoundboardSound{ID: "created", Name: "new"}
			return CreateSoundboardSoundResponse{SoundID: "created"}, nil
		}, refresh: func(current [soundboardSoundCount]SoundboardSound) {
			refreshed = true
			state.Resolve([]SoundboardSoundWithOrdinal{{SoundboardSound: current[0], ordinal: 0}})
		}}
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest("POST", "/swap-sound", strings.NewReader(`{"add":{"soundLocation":"new.ogg"}}`)))
		if result.Code != 200 || !refreshed || state.Busy(0) {
			t.Fatal("early Gateway result left loading stuck")
		}
	})
}

func TestSharedLoadingOldCompletionCannotClearNewOperation(t *testing.T) {
	state := newSharedSwitches(time.Second, func([]byte) {})
	first, err := state.Begin(0, "first")
	if err != nil {
		t.Fatal(err)
	}
	state.Remove(0, first)
	second, err := state.Begin(0, "second")
	if err != nil {
		t.Fatal(err)
	}
	state.Remove(0, first)
	current := decodeSharedState(t, state.Frame())
	if len(current.Pending) != 1 || current.Pending[0].Name != "second" {
		t.Fatal("stale completion cleared new upload")
	}
	state.Remove(0, second)
}

func TestSharedLoadingReachesTwoWebSocketViewersAndLateJoin(t *testing.T) {
	var viewersMu sync.Mutex
	viewers := make(map[*websocket.Conn]bool)
	state := newSharedSwitches(time.Second, func(frame []byte) {
		viewersMu.Lock()
		defer viewersMu.Unlock()
		for viewer := range viewers {
			if err := viewer.WriteMessage(websocket.TextMessage, frame); err != nil {
				t.Error(err)
			}
		}
	})
	entered, release := make(chan struct{}), make(chan struct{})
	handler := &trackedSwapHandler{state: state, board: func() [soundboardSoundCount]SoundboardSound { return [soundboardSoundCount]SoundboardSound{} }, execute: func(swapSoundInput) (CreateSoundboardSoundResponse, error) {
		close(entered)
		<-release
		return CreateSoundboardSoundResponse{}, errors.New("test upload failed")
	}}
	mux := http.NewServeMux()
	mux.Handle("/swap-sound", handler)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		viewer, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer viewer.Close()
		viewersMu.Lock()
		viewers[viewer] = true
		err = viewer.WriteMessage(websocket.TextMessage, state.Frame())
		viewersMu.Unlock()
		defer func() { viewersMu.Lock(); delete(viewers, viewer); viewersMu.Unlock() }()
		if err != nil {
			t.Error(err)
			return
		}
		for {
			if _, _, err := viewer.ReadMessage(); err != nil {
				return
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	dial := func() *websocket.Conn {
		t.Helper()
		viewer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		viewer.SetReadDeadline(time.Now().Add(3 * time.Second))
		return viewer
	}
	read := func(viewer *websocket.Conn) SharedSwitchSnapshot {
		t.Helper()
		_, data, err := viewer.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		return decodeSharedState(t, data)
	}
	first, second := dial(), dial()
	defer first.Close()
	defer second.Close()
	if len(read(first).Pending) != 0 || len(read(second).Pending) != 0 {
		t.Fatal("unexpected initial busy state")
	}
	finished := make(chan int, 1)
	go func() {
		response, err := http.Post(server.URL+"/swap-sound", "application/json", strings.NewReader(`{"slot":0,"add":{"soundLocation":"new.ogg"}}`))
		if err != nil {
			finished <- 0
			return
		}
		response.Body.Close()
		finished <- response.StatusCode
	}()
	<-entered
	for _, viewer := range []*websocket.Conn{first, second} {
		if state := read(viewer); len(state.Pending) != 1 || state.Pending[0].Name != "new" {
			t.Fatalf("viewer missed loading: %+v", state)
		}
	}
	late := dial()
	defer late.Close()
	if len(read(late).Pending) != 1 {
		t.Fatal("late WebSocket viewer missed active loading")
	}
	close(release)
	for _, viewer := range []*websocket.Conn{first, second, late} {
		if len(read(viewer).Pending) != 0 {
			t.Fatal("failure did not clear observer loading")
		}
	}
	if status := <-finished; status != 500 {
		t.Fatalf("HTTP status=%d", status)
	}
}
