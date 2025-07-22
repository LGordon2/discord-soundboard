package main

import (
	"fmt"
	"os"
	"path"
	"strings"
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

func addSound(discordClient *DiscordRestClient, storedSoundMap map[string][]byte, input addSoundInput) error {
	soundLocation := input.SoundLocation
	ext := path.Ext(soundLocation)
	nameWithoutExt := strings.TrimSuffix(soundLocation, ext)
	var data []byte
	if soundData, ok := storedSoundMap[nameWithoutExt]; ok {
		data = soundData
	} else {
		p := path.Join(soundsDir, soundLocation)
		fileData, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("[error] trouble reading file %v", p)
		}
		data = fileData
	}

	_, err := discordClient.CreateSoundboardSound(guildID, nameWithoutExt, "audio/"+ext, data)
	if err != nil {
		return fmt.Errorf("[error] creating soundboard sound for %s %v", soundLocation, err)
	}
	return nil
}
