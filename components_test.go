package main

import (
	"html"
	"strings"
	"testing"
)

func TestNativeSoundControls(t *testing.T) {
	name := `Rock & "roll" <script>alert(1)</script>`
	button := deleteButton("sound-id", "guild-id", "user", "", false)
	card := soundCardComponent(2, "sound-id", name, true, true, true, button)
	for _, action := range []string{"preview", "send", "save", "delete"} {
		if !strings.Contains(card, `data-action="`+action+`"`) {
			t.Errorf("missing action %s", action)
		}
	}
	for _, unwanted := range []string{"hx-", "htmx:", "<script>"} {
		if strings.Contains(card, unwanted) {
			t.Errorf("unexpected executable markup %q", unwanted)
		}
	}
	if !strings.Contains(card, `id="soundboard-2"`) || !strings.Contains(card, `data-soundname="`+html.EscapeString(name)+`"`) {
		t.Fatalf("missing ID or escaped name: %s", card)
	}
	if strings.Contains(card, "&lt;button") {
		t.Fatal("internally generated delete button was escaped")
	}
}

func TestNativeStoredAndEmptyCards(t *testing.T) {
	name := `name & "quotes"`
	stored := addSoundCardComponent(name, ".ogg", "guild", false)
	for _, want := range []string{`draggable="true"`, `data-action="add"`, `data-soundext=".ogg"`, `data-soundname="` + html.EscapeString(name) + `"`} {
		if !strings.Contains(stored, want) {
			t.Errorf("missing %s", want)
		}
	}
	empty := soundCardComponent(0, "", "", false, false, true, nil)
	if !strings.Contains(empty, "droppable") || !strings.Contains(empty, "Free") || strings.Contains(empty, `data-action="send"`) {
		t.Fatalf("incorrect empty card: %s", empty)
	}
	locked := soundCardComponent(0, "sound", "name", false, false, false, deleteButton("sound", "guild", "user", "", true))
	if strings.Contains(locked, " droppable") || strings.Count(locked, `disabled="true"`) != 1 || strings.Contains(locked, `data-action="save"`) || !strings.Contains(locked, `sound-download`) || !strings.Contains(locked, `data-guildid="guild" disabled`) {
		t.Fatalf("permissions not preserved: %s", locked)
	}
}
