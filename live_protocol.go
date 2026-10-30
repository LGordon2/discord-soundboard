package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
)

type liveSlot struct {
	Slot      int    `json:"slot"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	CanSend   bool   `json:"canSend,omitempty"`
	CanSave   bool   `json:"canSave,omitempty"`
	CanRemove bool   `json:"canRemove,omitempty"`
	Username  string `json:"username,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
	Guild     string `json:"guild,omitempty"`
}
type liveFile struct {
	Name string `json:"name"`
	Ext  string `json:"ext"`
}
type liveEvent struct {
	Type     string                `json:"type"`
	Slots    []liveSlot            `json:"slots,omitempty"`
	Full     bool                  `json:"full,omitempty"`
	Hidden   []string              `json:"hidden,omitempty"`
	HasEmpty bool                  `json:"hasEmpty,omitempty"`
	Library  *[]liveFile           `json:"library,omitempty"`
	Switches *SharedSwitchSnapshot `json:"switches,omitempty"`
	SoundID  string                `json:"soundID,omitempty"`
	Count    int                   `json:"count,omitempty"`
}

// Newline-delimited events allow a board update and its loading-state completion
// to travel in one WebSocket message and be applied atomically by the client.
func liveFrame(event liveEvent) []byte {
	data, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

// Keep already-open, cached clients working during a deployment. New clients
// request JSON explicitly; this adapter is used only by legacy connections.
func legacyLiveFrame(data []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event liveEvent
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		switch event.Type {
		case "board":
			if event.Full {
				out.WriteString(`<div id="playable-sounds" role="region" aria-label="Uploaded soundboard">`)
			}
			for _, s := range event.Slots {
				var button any
				if s.ID != "" {
					button = deleteButton(s.ID, s.Guild, s.Username, s.Avatar, !s.CanRemove)
				}
				out.WriteString(soundCardComponent(s.Slot, s.ID, s.Name, s.CanSend, s.CanSave, s.CanRemove, button))
			}
			if event.Full {
				out.WriteString(`</div>`)
			}
			hidden := event.Hidden
			if hidden == nil {
				hidden = []string{}
			}
			names, _ := json.Marshal(hidden)
			fmt.Fprintf(&out, `<div id="addsoundscript" data-hidden-sounds="%s" data-has-empty="%t"></div>`, html.EscapeString(string(names)), event.HasEmpty)
			if event.Library != nil {
				out.WriteString(`<div id="storedsounds" role="region" aria-label="Sound library">`)
				for _, file := range *event.Library {
					out.WriteString(addSoundCardComponent(file.Name, file.Ext, "", false))
				}
				out.WriteString(`</div>`)
			}
		case "switches":
			value, _ := json.Marshal(event.Switches)
			fmt.Fprintf(&out, `<div id="switchstate" data-value="%s"></div>`, html.EscapeString(string(value)))
		case "metadata":
			out.WriteString(`<div id="soundmetadata" data-refresh="true"></div>`)
		case "played":
			fmt.Fprintf(&out, `<div id="playsound" data-play-sound="%s"></div>`, html.EscapeString(event.SoundID))
		case "viewers":
			fmt.Fprintf(&out, `<span id="user-count">%d</span>`, event.Count)
		}
	}
	return out.Bytes()
}

// Version 2 packs the large library as [name, extension] tuples. Leave small
// live events untouched, and retain object entries for version 1 clients.
func compactLiveFrame(data []byte) []byte {
	if !bytes.Contains(data, []byte(`"library":`)) {
		return data
	}
	var out bytes.Buffer
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event map[string]json.RawMessage
		if err := json.Unmarshal(line, &event); err != nil {
			panic(err)
		}
		if library, ok := event["library"]; ok {
			var files []liveFile
			if err := json.Unmarshal(library, &files); err != nil {
				panic(err)
			}
			tuples := make([][2]string, 0, len(files))
			for _, file := range files {
				tuples = append(tuples, [2]string{file.Name, file.Ext})
			}
			packed, err := json.Marshal(tuples)
			if err != nil {
				panic(err)
			}
			event["library"] = packed
			line, err = json.Marshal(event)
			if err != nil {
				panic(err)
			}
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}
