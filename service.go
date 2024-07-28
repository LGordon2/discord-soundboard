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
	nameWithoutExt := strings.Split(soundLocation, ".")[0]
	var data []byte
	if soundData, ok := storedSoundMap[nameWithoutExt]; ok {
		data = soundData
	} else {
		fileData, err := os.ReadFile(path.Join(soundsDir, soundLocation))
		if err != nil {
			return fmt.Errorf("[error] trouble reading file %s", soundLocation)
		}
		data = fileData
	}

	arr := strings.Split(soundLocation, "/")
	nameAndExt := arr[len(arr)-1]

	arr = strings.Split(nameAndExt, ".")
	name := arr[0]
	extension := arr[len(arr)-1]

	_, err := discordClient.CreateSoundboardSound(guildID, name, "audio/"+extension, data)
	if err != nil {
		return fmt.Errorf("[error] creating soundboard sound for %s %v", soundLocation, err)
	}
	return nil
}
