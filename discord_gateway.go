package main

import (
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/gorilla/websocket"
)

const gatewayPayloadLimit = 16 << 20

func discordGatewayURL(compression string) (string, bool, error) {
	query := url.Values{"encoding": {"json"}, "v": {"9"}}
	switch compression {
	case "", "zlib-stream":
		query.Set("compress", "zlib-stream")
	case "none":
		return "wss://gateway.discord.gg/?" + query.Encode(), false, nil
	default:
		return "", false, fmt.Errorf("unsupported DISCORD_GATEWAY_COMPRESSION %q (use zlib-stream or none)", compression)
	}
	return "wss://gateway.discord.gg/?" + query.Encode(), true, nil
}

// Discord uses a single zlib stream for the entire connection, not WebSocket
// permessage-deflate or independent zlib payloads. NextReader handles WebSocket
// fragmentation; this reader also joins separate binary messages so a split
// Z_SYNC_FLUSH marker and the 32 KiB compression history survive intact.
type gatewayBinaryStream struct {
	conn     *websocket.Conn
	current  io.Reader
	received int64
}

func (s *gatewayBinaryStream) Read(p []byte) (int, error) {
	for {
		if s.current == nil {
			kind, reader, err := s.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if kind != websocket.BinaryMessage {
				return 0, fmt.Errorf("expected binary Discord zlib-stream payload, got WebSocket type %d", kind)
			}
			s.current = reader
		}
		n, err := s.current.Read(p)
		s.received += int64(n)
		if err == io.EOF {
			s.current = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

// Bound decompressed reads per Decode, rather than limiting the lifetime stream.
// The JSON decoder may retain a small amount of lookahead across payloads.
type gatewayInflatedStream struct {
	reader    io.Reader
	remaining int64
	decoded   int64
}

func (s *gatewayInflatedStream) Read(p []byte) (int, error) {
	if s.remaining <= 0 {
		return 0, fmt.Errorf("Discord Gateway payload exceeds decompression limit")
	}
	if int64(len(p)) > s.remaining {
		p = p[:s.remaining]
	}
	n, err := s.reader.Read(p)
	s.remaining -= int64(n)
	s.decoded += int64(n)
	return n, err
}

type discordGatewayReader struct {
	conn       *websocket.Conn
	compressed bool
	wire       *gatewayBinaryStream
	inflater   io.ReadCloser
	inflated   *gatewayInflatedStream
	decoder    *json.Decoder
}

func newDiscordGatewayReader(conn *websocket.Conn, compressed bool) *discordGatewayReader {
	conn.SetReadLimit(gatewayPayloadLimit)
	return &discordGatewayReader{conn: conn, compressed: compressed}
}
func (r *discordGatewayReader) ReadJSON(value any) error {
	if !r.compressed {
		return r.conn.ReadJSON(value)
	}
	if r.decoder == nil {
		r.wire = &gatewayBinaryStream{conn: r.conn}
		inflater, err := zlib.NewReader(r.wire)
		if err != nil {
			return fmt.Errorf("initialize Discord zlib stream: %w", err)
		}
		r.inflater = inflater
		r.inflated = &gatewayInflatedStream{reader: inflater}
		r.decoder = json.NewDecoder(r.inflated)
	}
	r.inflated.remaining = gatewayPayloadLimit
	if err := r.decoder.Decode(value); err != nil {
		return fmt.Errorf("decode Discord zlib stream: %w", err)
	}
	return nil
}
func (r *discordGatewayReader) Close() {
	if r.inflater != nil {
		r.inflater.Close()
	}
}
func (r *discordGatewayReader) Stats() (wire, decoded int64) {
	if r.wire != nil {
		wire = r.wire.received
	}
	if r.inflated != nil {
		decoded = r.inflated.decoded
	}
	return
}
