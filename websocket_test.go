package main

import (
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

type countedConnection struct {
	net.Conn
	received atomic.Int64
}

func (c *countedConnection) Read(data []byte) (int, error) {
	n, err := c.Conn.Read(data)
	c.received.Add(int64(n))
	return n, err
}

func TestWebSocketCompressionPreservesLargeSnapshotAndShrinksTransfer(t *testing.T) {
	var payload bytes.Buffer
	payload.WriteString(`<div id="storedsounds">`)
	for i := 0; i < 600; i++ {
		payload.WriteString(addSoundCardComponent(fmt.Sprintf("Sound %d", i), ".ogg", guildID, false))
	}
	payload.WriteString(`</div>`)
	transfer := func(compress bool) int64 {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			connection, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer connection.Close()
			connection.SetCompressionLevel(flate.BestSpeed)
			if err := connection.WriteMessage(websocket.TextMessage, payload.Bytes()); err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()
		var wire *countedConnection
		dialer := websocket.Dialer{EnableCompression: compress, NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			wire = &countedConnection{Conn: connection}
			return wire, nil
		}}
		connection, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		if negotiated := strings.Contains(response.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate"); negotiated != compress {
			t.Fatalf("compression negotiated=%v", negotiated)
		}
		_, message, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(message, payload.Bytes()) {
			t.Fatal("snapshot changed in transit")
		}
		return wire.received.Load()
	}
	plain, compressed := transfer(false), transfer(true)
	if compressed >= plain/2 {
		t.Fatalf("ineffective compression: %d vs %d", compressed, plain)
	}
	t.Logf("600-card transfer including handshake: %d bytes plain, %d compressed (%.1f%% smaller)", plain, compressed, 100*(1-float64(compressed)/float64(plain)))
}
