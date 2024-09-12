package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	baseURL         = "https://discord.com"
	superProperties = "eyJvcyI6IldpbmRvd3MiLCJicm93c2VyIjoiQ2hyb21lIiwiZGV2aWNlIjoiIiwic3lzdGVtX2xvY2FsZSI6ImVuLVVTIiwiYnJvd3Nlcl91c2VyX2FnZW50IjoiTW96aWxsYS81LjAgKFdpbmRvd3MgTlQgMTAuMDsgV2luNjQ7IHg2NCkgQXBwbGVXZWJLaXQvNTM3LjM2IChLSFRNTCwgbGlrZSBHZWNrbykgQ2hyb21lLzEyNS4wLjAuMCBTYWZhcmkvNTM3LjM2IiwiYnJvd3Nlcl92ZXJzaW9uIjoiMTI1LjAuMC4wIiwib3NfdmVyc2lvbiI6IjEwIiwicmVmZXJyZXIiOiJodHRwczovL3d3dy5nb29nbGUuY29tLyIsInJlZmVycmluZ19kb21haW4iOiJ3d3cuZ29vZ2xlLmNvbSIsInNlYXJjaF9lbmdpbmUiOiJnb29nbGUiLCJyZWZlcnJlcl9jdXJyZW50IjoiIiwicmVmZXJyaW5nX2RvbWFpbl9jdXJyZW50IjoiIiwicmVsZWFzZV9jaGFubmVsIjoic3RhYmxlIiwiY2xpZW50X2J1aWxkX251bWJlciI6MzAxOTIwLCJjbGllbnRfZXZlbnRfc291cmNlIjpudWxsLCJkZXNpZ25faWQiOjB9"
)

type DiscordRestClient struct {
	token   string
	discord *discordgo.Session
	userID  string
}

func NewDiscordRestClient(token, tokenType string) *DiscordRestClient {
	// if tokenType == "" {
	// 	tokenType = "Bot"
	// }
	discord, err := discordgo.New(tokenType + token)
	if err != nil {
		panic(err)
	}
	u, err := discord.User("@me")
	if err != nil {
		panic(err)
	}
	return &DiscordRestClient{
		token:   token,
		discord: discord,
		userID:  u.ID,
	}
}

func (c *DiscordRestClient) GetUserId() string {
	return c.userID
}

type SendSoundboardSoundRequest struct {
	SoundID       string  `json:"sound_id"`
	EmojiID       *string `json:"emoji_id"`
	SourceGuildID string  `json:"source_guild_id"`
}

func (c *DiscordRestClient) SendSoundboardSound(guildId, channelId, soundId string) error {
	_, err := c.discord.Request(http.MethodPost, baseURL+"/api/v9/channels/"+channelId+"/send-soundboard-sound", SendSoundboardSoundRequest{
		SoundID:       soundId,
		EmojiID:       nil,
		SourceGuildID: guildId,
	}, func(cfg *discordgo.RequestConfig) {
		cfg.Request.Header.Set("X-Super-Properties", superProperties)
	})
	return err
}

func (c *DiscordRestClient) DeleteSoundboardSound(guildId, soundId string) error {
	start := time.Now()
	_, err := c.discord.Request(http.MethodDelete, baseURL+"/api/v9/guilds/"+guildId+"/soundboard-sounds/"+soundId, nil, func(cfg *discordgo.RequestConfig) {
		cfg.Request.Header.Set("X-Super-Properties", superProperties)
	})
	fmt.Printf("DeleteSoundboardSound: %v\n", time.Since(start))
	return err
}

type CreateSoundboardSoundRequest struct {
	Name  string `json:"name"`
	Sound string `json:"sound"`
}

type CreateSoundboardSoundResponse struct {
	Name    string  `json:"name"`
	SoundID string  `json:"sound_id"`
	ID      string  `json:"id"`
	Volume  float32 `json:"volume"`
	// There are more, but I'm too lazy to add them.
}

func (c *DiscordRestClient) CreateSoundboardSound(guildId, name, mimeType string, data []byte) (CreateSoundboardSoundResponse, error) {
	var soundBuf bytes.Buffer
	soundBuf.WriteString("data:" + mimeType + ";base64,")
	soundBuf.WriteString(base64.StdEncoding.EncodeToString(data))

	start := time.Now()
	resp, err := c.discord.Request(http.MethodPost, baseURL+"/api/v9/guilds/"+guildId+"/soundboard-sounds", CreateSoundboardSoundRequest{
		Name:  name,
		Sound: soundBuf.String(),
	}, func(cfg *discordgo.RequestConfig) {
		cfg.Request.Header.Set("X-Super-Properties", superProperties)
	})
	if err != nil {
		return CreateSoundboardSoundResponse{}, err
	}
	fmt.Printf("CreateSoundboardSound: %v\n", time.Since(start))

	var soundboardResponse CreateSoundboardSoundResponse
	err = json.Unmarshal(resp, &soundboardResponse)
	return soundboardResponse, err
}
