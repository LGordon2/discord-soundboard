package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestFullBoardReplacementWithoutDeleteSnapshot(t *testing.T) {
	var previous [soundboardSoundCount]SoundboardSound
	incoming := make([]SoundboardSound, 0)
	for i := range previous {
		previous[i] = SoundboardSound{ID: fmt.Sprint(i + 1), Name: fmt.Sprint(i + 1)}
		if i != 3 {
			incoming = append(incoming, previous[i])
		}
	}
	replacement := SoundboardSound{ID: "new-id", Name: "new"}
	// New sound arrives before the retained IDs in Discord's snapshot.
	incoming = append([]SoundboardSound{replacement}, incoming...)
	next, updates := reconcileSounds(previous, incoming)
	expected := previous
	expected[3] = replacement
	if next != expected {
		t.Fatalf("replacement lost or existing slots moved: %+v", next)
	}
	if len(updates) != 1 || updates[0].ordinal != 3 || updates[0].SoundboardSound != replacement {
		t.Fatalf("incorrect replacement updates: %+v", updates)
	}
}

func TestSoundStateChangesAndRepeatedSnapshot(t *testing.T) {
	var previous [soundboardSoundCount]SoundboardSound
	previous[1] = SoundboardSound{ID: "keep", Name: "old"}
	previous[4] = SoundboardSound{ID: "remove", Name: "removed"}
	renamed := SoundboardSound{ID: "keep", Name: "renamed", Avatar: "new-avatar"}
	next, updates := reconcileSounds(previous, []SoundboardSound{renamed})
	if next[1] != renamed || next[4] != (SoundboardSound{}) {
		t.Fatalf("incorrect state: %+v", next)
	}
	var changed []int
	for _, update := range updates {
		changed = append(changed, update.ordinal)
	}
	if !reflect.DeepEqual(changed, []int{1, 4}) {
		t.Fatalf("incorrect changed slots: %v", changed)
	}
	repeated, changes := reconcileSounds(next, []SoundboardSound{renamed})
	if repeated != next || len(changes) != 0 {
		t.Fatalf("unchanged snapshot generated updates: %+v", changes)
	}
}

func TestSoundStateIgnoresDuplicateIDs(t *testing.T) {
	sound := SoundboardSound{ID: "new", Name: "new"}
	var previous [soundboardSoundCount]SoundboardSound
	next, updates := reconcileSounds(previous, []SoundboardSound{sound, sound, {}})
	if next[0] != sound || len(updates) != 1 || next[1] != (SoundboardSound{}) {
		t.Fatalf("duplicate slots: %+v", next)
	}
}
