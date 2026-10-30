package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	baseURL         = "https://discord.com"
	superProperties = "eyJvcyI6IldpbmRvd3MiLCJicm93c2VyIjoiQ2hyb21lIiwiZGV2aWNlIjoiIiwic3lzdGVtX2xvY2FsZSI6ImVuLVVTIiwiYnJvd3Nlcl91c2VyX2FnZW50IjoiTW96aWxsYS81LjAgKFdpbmRvd3MgTlQgMTAuMDsgV2luNjQ7IHg2NCkgQXBwbGVXZWJLaXQvNTM3LjM2IChLSFRNTCwgbGlrZSBHZWNrbykgQ2hyb21lLzEyNS4wLjAuMCBTYWZhcmkvNTM3LjM2IiwiYnJvd3Nlcl92ZXJzaW9uIjoiMTI1LjAuMC4wIiwib3NfdmVyc2lvbiI6IjEwIiwicmVmZXJyZXIiOiJodHRwczovL3d3dy5nb29nbGUuY29tLyIsInJlZmVycmluZ19kb21haW4iOiJ3d3cuZ29vZ2xlLmNvbSIsInNlYXJjaF9lbmdpbmUiOiJnb29nbGUiLCJyZWZlcnJlcl9jdXJyZW50IjoiIiwicmVmZXJyaW5nX2RvbWFpbl9jdXJyZW50IjoiIiwicmVsZWFzZV9jaGFubmVsIjoic3RhYmxlIiwiY2xpZW50X2J1aWxkX251bWJlciI6MzAxOTIwLCJjbGllbnRfZXZlbnRfc291cmNlIjpudWxsLCJkZXNpZ25faWQiOjB9"
)

type DiscordRestClient struct {
	token          string
	discord        *discordgo.Session
	userID         string
	onSoundCreated func(name string)
	onSoundSent    func(soundID string)
	metadata       *SoundMetadataStore
}

func NewDiscordRestClient(token, tokenType string) *DiscordRestClient {
	// if tokenType == "" {
	// 	tokenType = "Bot"
	// }
	discord, err := discordgo.New(tokenType + token)
	if err != nil {
		panic(err)
	}
	transport := discord.Client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	discord.Client.Transport = discordTimingTransport{base: transport}
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

// Log individual HTTP attempts separately from the end-to-end method timers,
// which also include DiscordGo's bucket waits and automatic retries.
type discordTimingTransport struct{ base http.RoundTripper }

func (t discordTimingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	if resp != nil {
		log.Printf("discord HTTP method=%s status=%d duration=%v remaining=%q reset_after=%q retry_after=%q bucket=%q", req.Method, resp.StatusCode, time.Since(start), resp.Header.Get("X-RateLimit-Remaining"), resp.Header.Get("X-RateLimit-Reset-After"), resp.Header.Get("Retry-After"), resp.Header.Get("X-RateLimit-Bucket"))
	} else {
		log.Printf("discord HTTP method=%s duration=%v failed=true", req.Method, time.Since(start))
	}
	return resp, err
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
	if err == nil && c.onSoundSent != nil {
		c.onSoundSent(soundId)
	}
	return err
}

func (c *DiscordRestClient) DeleteSoundboardSound(guildId, soundId string) error {
	start := time.Now()
	defer func() { fmt.Printf("DeleteSoundboardSound (including rate-limit waits): %v\n", time.Since(start)) }()
	_, err := c.discord.Request(http.MethodDelete, baseURL+"/api/v9/guilds/"+guildId+"/soundboard-sounds/"+soundId, nil, func(cfg *discordgo.RequestConfig) {
		cfg.Request.Header.Set("X-Super-Properties", superProperties)
	})
	return err
}

type CreateSoundboardSoundRequest struct {
	Name      string  `json:"name"`
	Sound     string  `json:"sound"`
	Volume    int     `json:"volume"`
	EmojiID   *string `json:"emoji_id,omitempty"`
	EmojiName *string `json:"emoji_name,omitempty"`
}

type CreateSoundboardSoundResponse struct {
	Name    string  `json:"name"`
	SoundID string  `json:"sound_id"`
	ID      string  `json:"id"`
	Volume  float32 `json:"volume"`
	// There are more, but I'm too lazy to add them.
}

func (c *DiscordRestClient) CreateSoundboardSound(guildId, name, mimeType string, data []byte) (CreateSoundboardSoundResponse, error) {
	return c.createSoundboardSound(guildId, prepareSoundboardSound(name, mimeType, data))
}

// Prepare uploads while loading the local library, rather than base64 encoding
// the audio on every replacement after the old Discord sound has been deleted.
func prepareSoundboardSound(name, mimeType string, data []byte) CreateSoundboardSoundRequest {
	return CreateSoundboardSoundRequest{
		Name:   name,
		Sound:  "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
		Volume: 1,
	}
}

func (c *DiscordRestClient) createSoundboardSound(guildId string, request CreateSoundboardSoundRequest) (CreateSoundboardSoundResponse, error) {
	if c.metadata != nil {
		request = c.metadata.ApplyEmoji(request)
	}
	start := time.Now()
	defer func() { fmt.Printf("CreateSoundboardSound (including rate-limit waits): %v\n", time.Since(start)) }()
	post := func() ([]byte, error) {
		return c.discord.Request(http.MethodPost, baseURL+"/api/v9/guilds/"+guildId+"/soundboard-sounds", request, func(cfg *discordgo.RequestConfig) {
			cfg.Request.Header.Set("X-Super-Properties", superProperties)
		})
	}
	resp, err := post()
	if err != nil && (request.EmojiID != nil || request.EmojiName != nil) && invalidSoundEmoji(err) {
		// A deleted/unavailable custom emoji must not prevent restoring the audio.
		request.EmojiID, request.EmojiName = nil, nil
		if c.metadata != nil {
			c.metadata.setEmoji(request.Name, nil, nil, true)
		}
		log.Printf("[warn] Discord rejected a stored emoji; retrying the sound upload without it")
		resp, err = post()
	}
	if err != nil {
		return CreateSoundboardSoundResponse{}, err
	}

	var soundboardResponse CreateSoundboardSoundResponse
	err = json.Unmarshal(resp, &soundboardResponse)
	if err == nil && c.onSoundCreated != nil {
		c.onSoundCreated(request.Name)
	}
	return soundboardResponse, err
}

func invalidSoundEmoji(err error) bool {
	var restError *discordgo.RESTError
	if !errors.As(err, &restError) || restError.Response == nil || restError.Response.StatusCode != http.StatusBadRequest {
		return false
	}
	var details struct {
		Errors map[string]json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(restError.ResponseBody, &details) != nil {
		return false
	}
	_, custom := details.Errors["emoji_id"]
	_, unicode := details.Errors["emoji_name"]
	return custom || unicode
}
