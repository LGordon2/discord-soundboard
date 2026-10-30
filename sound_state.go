package main

import "sync/atomic"

// Reconcile a complete Discord snapshot before assigning new sounds. Slots
// occupied by removed IDs are available even if no delete-only snapshot arrived.
func reconcileSounds(previous [soundboardSoundCount]SoundboardSound, incoming []SoundboardSound) ([soundboardSoundCount]SoundboardSound, []SoundboardSoundWithOrdinal) {
	var next [soundboardSoundCount]SoundboardSound
	positions := make(map[string]int)
	for i, sound := range previous {
		if sound.ID != "" {
			positions[sound.ID] = i
		}
	}
	seen := make(map[string]bool)
	additions := make([]SoundboardSound, 0)
	for _, sound := range incoming {
		if sound.ID == "" || seen[sound.ID] {
			continue
		}
		seen[sound.ID] = true
		if pos, ok := positions[sound.ID]; ok {
			next[pos] = sound
		} else {
			additions = append(additions, sound)
		}
	}
	for _, sound := range additions {
		for i := range next {
			if next[i].ID == "" {
				next[i] = sound
				break
			}
		}
	}
	updates := make([]SoundboardSoundWithOrdinal, 0)
	for i, sound := range next {
		if sound != previous[i] {
			updates = append(updates, SoundboardSoundWithOrdinal{SoundboardSound: sound, ordinal: i})
		}
	}
	return next, updates
}

// Other members moving, self-muted events, and repeated notifications cannot
// change these controls. Avoid rebuilding the board for those events.
func applyVoiceStateUpdate(current *atomic.Bool, event DiscordMessageData, selfID, serverID, targetChannelID string) bool {
	if event.UserID != selfID || event.GuildID != serverID {
		return false
	}
	next := event.ChannelID == targetChannelID
	return current.Swap(next) != next
}
