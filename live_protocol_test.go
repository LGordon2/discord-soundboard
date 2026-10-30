package main

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func compressedSize(data []byte) int {
	var b bytes.Buffer
	w, _ := flate.NewWriter(&b, flate.BestSpeed)
	w.Write(data)
	w.Close()
	return b.Len()
}
func TestLiveProtocolSnapshotAndCompatibility(t *testing.T) {
	files := []liveFile{}
	for i := 0; i < 596; i++ {
		files = append(files, liveFile{Name: fmt.Sprintf("Sound %d & quote", i), Ext: ".ogg"})
	}
	event := liveEvent{Type: "board", Full: true, Library: &files, HasEmpty: true, Hidden: []string{"Live & sound"}}
	for i := 0; i < 8; i++ {
		event.Slots = append(event.Slots, liveSlot{Slot: i, CanRemove: true})
	}
	wire := liveFrame(event)
	legacy := legacyLiveFrame(wire)
	if !bytes.Contains(legacy, []byte(`id="playable-sounds"`)) || !bytes.Contains(legacy, []byte(`id="storedsounds"`)) {
		t.Fatal("legacy snapshot regions missing")
	}
	if bytes.Count(legacy, []byte(`draggable="true"`)) != 596 {
		t.Fatal("library lost")
	}
	var decoded liveEvent
	if err := json.Unmarshal(bytes.TrimSpace(wire), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(*decoded.Library) != 596 || len(decoded.Slots) != 8 {
		t.Fatal("JSON snapshot lost sounds")
	}
	if len(wire) >= len(legacy) || compressedSize(wire) >= compressedSize(legacy) {
		t.Fatal("JSON should reduce both raw and compressed snapshot size")
	}
	t.Logf("596-sound snapshot: HTML %d bytes / %d deflated; JSON %d bytes / %d deflated", len(legacy), compressedSize(legacy), len(wire), compressedSize(wire))
	slot := liveFrame(liveEvent{Type: "board", Slots: []liveSlot{{Slot: 2, ID: "123456789012345678", Name: "Airhorn", CanSend: true, CanRemove: true, Guild: "123456789012345678"}}, HasEmpty: true, Hidden: []string{"Airhorn"}})
	t.Logf("slot update: HTML %d bytes / %d deflated; JSON %d bytes / %d deflated", len(legacyLiveFrame(slot)), compressedSize(legacyLiveFrame(slot)), len(slot), compressedSize(slot))
}
func TestLiveProtocolAtomicBatchAndEscaping(t *testing.T) {
	data := liveFrame(liveEvent{Type: "board", Slots: []liveSlot{{Slot: 2, Name: `<script>"&`, CanRemove: true}}, HasEmpty: true})
	data = append(data, switchStateFrame(SharedSwitchSnapshot{Epoch: "test", Version: 2, Pending: []SharedSwitch{}})...)
	data = append(data, liveFrame(liveEvent{Type: "played", SoundID: `"><script>`})...)
	data = append(data, liveFrame(liveEvent{Type: "viewers", Count: 3})...)
	data = append(data, liveFrame(liveEvent{Type: "metadata"})...)
	legacy := string(legacyLiveFrame(data))
	if strings.Contains(legacy, "<script>") {
		t.Fatal("unescaped legacy data")
	}
	for _, id := range []string{"soundboard-2", "switchstate", "playsound", "user-count", "soundmetadata"} {
		if !strings.Contains(legacy, id) {
			t.Fatalf("batch missing %s", id)
		}
	}
}
