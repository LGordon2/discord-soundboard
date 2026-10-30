package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"
)

type deleteSoundInput struct {
	SoundID string `json:"soundID"`
}

func deleteSound(discordClient *DiscordRestClient, guildID string, input deleteSoundInput) error {
	err := discordClient.DeleteSoundboardSound(guildID, input.SoundID)
	if err != nil {
		return fmt.Errorf("[error] deleting file %v", err)
	}
	return nil
}

type addSoundInput struct {
	SoundLocation string `json:"soundLocation"`
}

// Cached uploads use the full filename so .ogg and .mp3 variants cannot collide.
type storedSoundMap map[string]CreateSoundboardSoundRequest

func (s storedSoundMap) containsName(name string) bool {
	_, ogg := s[name+".ogg"]
	_, mp3 := s[name+".mp3"]
	return ogg || mp3
}

func prepareStoredSound(storedSounds storedSoundMap, input addSoundInput) (CreateSoundboardSoundRequest, error) {
	location := input.SoundLocation
	if location != path.Base(location) || location == "." || location == ".." {
		return CreateSoundboardSoundRequest{}, fmt.Errorf("invalid sound filename %q", location)
	}
	ext := path.Ext(location)
	mimeType := "audio/ogg"
	switch ext {
	case ".mp3":
		mimeType = "audio/mpeg"
	case ".ogg":
	default:
		return CreateSoundboardSoundRequest{}, fmt.Errorf("unsupported sound extension %q", ext)
	}
	if request, ok := storedSounds[location]; ok {
		return request, nil
	}
	data, err := os.ReadFile(path.Join(soundsDir, location))
	if err != nil {
		return CreateSoundboardSoundRequest{}, fmt.Errorf("reading sound %q: %w", location, err)
	}
	if len(data) == 0 {
		return CreateSoundboardSoundRequest{}, fmt.Errorf("sound %q is empty", location)
	}
	return prepareSoundboardSound(strings.TrimSuffix(location, ext), mimeType, data), nil
}

func addSound(discordClient *DiscordRestClient, storedSounds storedSoundMap, input addSoundInput) error {
	request, err := prepareStoredSound(storedSounds, input)
	if err != nil {
		return err
	}
	_, err = discordClient.createSoundboardSound(guildID, request)
	if err != nil {
		return fmt.Errorf("creating soundboard sound for %s: %w", input.SoundLocation, err)
	}
	return nil
}

type swapSoundInput struct {
	trace  string
	Slot   *int             `json:"slot,omitempty"`
	Add    addSoundInput    `json:"add"`
	Delete deleteSoundInput `json:"delete"`
}

func swapSound(client *DiscordRestClient, storedSounds storedSoundMap, input swapSoundInput) error {
	_, err := swapSoundResult(client, storedSounds, input)
	return err
}

func swapSoundResult(client *DiscordRestClient, storedSounds storedSoundMap, input swapSoundInput) (CreateSoundboardSoundResponse, error) {
	// Finish all local preparation before freeing the Discord slot. An unreadable
	// replacement must not delete the current sound.
	var request CreateSoundboardSoundRequest
	var err error
	if input.Add != (addSoundInput{}) {
		started := time.Now()
		request, err = prepareStoredSound(storedSounds, input.Add)
		log.Printf("switch_timing operation=%s stage=prepare duration_ms=%.3f success=%t", input.trace, float64(time.Since(started))/float64(time.Millisecond), err == nil)
		if err != nil {
			return CreateSoundboardSoundResponse{}, err
		}
	}
	// The board can be full: creation must wait for successful deletion.
	if input.Delete != (deleteSoundInput{}) {
		started := time.Now()
		err := deleteSound(client, guildID, input.Delete)
		log.Printf("switch_timing operation=%s stage=delete duration_ms=%.3f success=%t", input.trace, float64(time.Since(started))/float64(time.Millisecond), err == nil)
		if err != nil {
			return CreateSoundboardSoundResponse{}, err
		}
	}
	if input.Add != (addSoundInput{}) {
		started := time.Now()
		result, err := client.createSoundboardSound(guildID, request)
		log.Printf("switch_timing operation=%s stage=create duration_ms=%.3f success=%t", input.trace, float64(time.Since(started))/float64(time.Millisecond), err == nil)
		return result, err
	}
	return CreateSoundboardSoundResponse{}, err
}
