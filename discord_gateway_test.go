package main

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func gatewayTestSocket(t *testing.T, serve func(*websocket.Conn)) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		serve(conn)
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	return conn
}
func TestDiscordGatewayCompressionConfiguration(t *testing.T) {
	for _, mode := range []string{"", "zlib-stream", "none"} {
		address, compressed, err := discordGatewayURL(mode)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		if compressed != (mode != "none") || parsed.Query().Get("encoding") != "json" {
			t.Fatal("incorrect config")
		}
		if compressed && parsed.Query().Get("compress") != "zlib-stream" {
			t.Fatal("compression not requested")
		}
	}
	if _, _, err := discordGatewayURL("broken"); err == nil {
		t.Fatal("invalid config accepted")
	}
}
func TestDiscordGatewaySharedStreamAndSplitFlush(t *testing.T) {
	// More than one event must share the compressor dictionary. Each flush is
	// delivered in several WebSocket messages, including a split flush suffix.
	var compressed bytes.Buffer
	compressor := zlib.NewWriter(&compressed)
	var payloads, chunks [][]byte
	for i := 0; i < 3; i++ {
		payload, _ := json.Marshal(map[string]any{"t": "SOUNDBOARD_SOUNDS", "d": map[string]any{"guild_id": "test", "names": strings.Repeat("a reusable sound name;", 500)}, "s": i})
		payloads = append(payloads, payload)
		compressor.Write(payload)
		compressor.Flush()
		chunks = append(chunks, bytes.Clone(compressed.Bytes()))
		compressed.Reset()
	}
	conn := gatewayTestSocket(t, func(conn *websocket.Conn) {
		for _, chunk := range chunks {
			for _, part := range [][]byte{chunk[:1], chunk[1 : len(chunk)-2], chunk[len(chunk)-2:]} {
				if conn.WriteMessage(websocket.BinaryMessage, part) != nil {
					return
				}
			}
		}
		// Keep the stream open without a zlib trailer: each event must decode now,
		// rather than waiting for the next flush or for the WebSocket to close.
		conn.ReadMessage()
	})
	reader := newDiscordGatewayReader(conn, true)
	defer reader.Close()
	for _, want := range payloads {
		var got json.RawMessage
		if err := reader.ReadJSON(&got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatal("decoded payload changed")
		}
	}
	wire, decoded := reader.Stats()
	if wire <= 0 || decoded <= wire {
		t.Fatalf("invalid compression stats: %d %d", wire, decoded)
	}
}
func TestDiscordGatewayFirstFlushDoesNotWaitForAnotherEvent(t *testing.T) {
	ready := make(chan struct{})
	conn := gatewayTestSocket(t, func(conn *websocket.Conn) {
		var buf bytes.Buffer
		compressor := zlib.NewWriter(&buf)
		compressor.Write([]byte(`{"op":10,"d":{"heartbeat_interval":45000}}`))
		compressor.Flush()
		conn.WriteMessage(websocket.BinaryMessage, buf.Bytes())
		<-ready
	})
	defer close(ready)
	reader := newDiscordGatewayReader(conn, true)
	defer reader.Close()
	var msg map[string]any
	if err := reader.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	if msg["op"] != float64(10) {
		t.Fatal("missing Hello")
	}
}
func TestDiscordGatewayDecodeErrorsAndFreshConnections(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
		kind int
	}{
		{"bad header", []byte("not zlib"), websocket.BinaryMessage},
		{"unexpected text", []byte(`{"op":10}`), websocket.TextMessage},
		{"truncated", []byte{0x78, 0x9c, 0x01}, websocket.BinaryMessage},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := gatewayTestSocket(t, func(conn *websocket.Conn) { conn.WriteMessage(test.kind, test.data) })
			reader := newDiscordGatewayReader(conn, true)
			defer reader.Close()
			var event any
			if err := reader.ReadJSON(&event); err == nil {
				t.Fatal("bad stream accepted")
			}
		})
	}
	for i := 0; i < 2; i++ {
		conn := gatewayTestSocket(t, func(conn *websocket.Conn) {
			var b bytes.Buffer
			w := zlib.NewWriter(&b)
			w.Write([]byte(`{"t":"READY","d":{}}`))
			w.Flush()
			conn.WriteMessage(websocket.BinaryMessage, b.Bytes())
			conn.ReadMessage()
		})
		reader := newDiscordGatewayReader(conn, true)
		var msg DiscordMessage
		if err := reader.ReadJSON(&msg); err != nil {
			t.Fatal(err)
		}
		reader.Close()
		conn.Close()
		if msg.Type == nil || *msg.Type != "READY" {
			t.Fatal("fresh connection failed")
		}
	}
}
func TestDiscordGatewayUncompressedRollbackAndReadLimit(t *testing.T) {
	conn := gatewayTestSocket(t, func(conn *websocket.Conn) { conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"READY","d":{}}`)) })
	reader := newDiscordGatewayReader(conn, false)
	var msg DiscordMessage
	if err := reader.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	limited := &gatewayInflatedStream{reader: strings.NewReader(strings.Repeat("a", 20)), remaining: 10}
	if data, err := io.ReadAll(limited); err == nil || len(data) != 10 {
		t.Fatal("decompression limit ineffective")
	}
}
