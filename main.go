package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/segmentio/encoding/json"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/html"
)

type UserData struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
}

type SoundData struct {
	SoundID string `json:"sound_id"`
	Name    string `json:"name"`
	UserID  string `json:"user_id"`
	User    UserData
}

type VoiceState struct {
	ChannelID string `json:"channel_id"`
}

type GuildData struct {
	ID          string       `json:"id"`
	VoiceStates []VoiceState `json:"voice_states"`
}

type DiscordMessageData struct {
	Guilds           []GuildData `json:"guilds"`
	UserID           string      `json:"user_id"`
	ChannelID        string      `json:"channel_id"`
	Users            []UserData  `json:"users"`
	GuildID          string      `json:"guild_id"`
	SoundboardSounds []SoundData `json:"soundboard_sounds"`
	Name             string      `json:"name"`
	SoundID          string      `json:"sound_id"`
}

type DiscordMessage struct {
	Type    *string     `json:"t"`
	GuildID *string     `json:"guild_id"`
	Data    interface{} `json:"d"`
}

type SoundboardSound struct {
	Name   string
	ID     string
	UserID string
	Avatar string
}

var (
	authToken string // grab from a discord API call
	soundsDir string // where you store sounds on the server (e.g. /home/user/sounds/...)
)

const guildID = "284709094588284929"   // Viznet
const channelID = "284709094588284930" // general channel
// const guildID = "752332599631806505"   // Faceclub
// const channelID = "752332599631806509" // general channel

const soundboardSoundCount = 8

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
} // use default options

// returns the list of sounds, and a map of sound name to sound data
func fetchStoredSounds() ([]string, map[string][]byte, error) {
	files, err := os.ReadDir(soundsDir)
	if err != nil {
		panic(err)
	}
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name()) < strings.ToLower(files[j].Name()) })

	storedSounds := []string{}
	storedSoundMap := make(map[string][]byte) // these won't contain the extension
	for _, f := range files {
		if !(strings.HasSuffix(f.Name(), ".ogg") || strings.HasSuffix(f.Name(), ".mp3")) {
			continue
		}
		storedSounds = append(storedSounds, f.Name())
		nameWithoutExt := strings.Split(f.Name(), ".")[0]
		data, err := os.ReadFile(path.Join(soundsDir, f.Name()))
		if err == nil {
			storedSoundMap[nameWithoutExt] = data
		} else {
			storedSoundMap[nameWithoutExt] = []byte{}

			fmt.Printf("[warn] couldn't prefetch file %s\n", f.Name())
		}
	}
	return storedSounds, storedSoundMap, nil
}

type SoundboardSoundWithOrdinal struct {
	SoundboardSound
	ordinal int
}

type UserInfo struct {
	UserID   string
	Username string
	Avatar   string
}

func main() {
	userInfoCache := make(map[string]UserInfo)

	m := minify.New()
	m.AddFunc("text/html", html.Minify)

	var userIsInChannel atomic.Bool
	userIsInChannel.Store(false)
	var mu sync.RWMutex
	sounds := [soundboardSoundCount]SoundboardSound{}
	storedSounds, storedSoundMap, err := fetchStoredSounds()
	if err != nil {
		panic(err)
	}
	discordClient := NewDiscordRestClient(authToken, "")

	msgUpdates := make(chan []byte, 100)
	clients := make(map[*websocket.Conn]chan []byte)
	updateStoredSounds := func(soundsWithOrdinal []SoundboardSoundWithOrdinal) *bytes.Buffer {
		var buf bytes.Buffer
		buf.WriteString("<div id=\"storedsounds\" class=\"flex flex-1 flex-wrap justify-center items-center max-w-7xl\">")
		for i, storedSound := range storedSounds {
			storedSoundNoExt := strings.Split(storedSound, ".")[0]
			soundData := storedSoundMap[storedSoundNoExt]
			// hide sounds already present on the sound map
			onSoundboard := false
			for _, soundWithOrdinal := range soundsWithOrdinal {
				if storedSoundNoExt == soundWithOrdinal.Name {
					onSoundboard = true
					break
				}
			}
			buf.WriteString(soundCardComponent(i, storedSoundNoExt, guildID, userIsInChannel.Load(), onSoundboard, soundData))
		}
		buf.WriteString("</div>")
		var minifiedBuf bytes.Buffer
		m.Minify("text/html", &minifiedBuf, &buf)
		return &minifiedBuf
	}

	// main go routine to update all clients
	go func() {
		for msgUpdate := range msgUpdates {
			mu.RLock()
			for _, c := range clients {
				c <- msgUpdate
			}
			mu.RUnlock()
		}
	}()
	saveSoundFunc := func(soundID, soundName string) error {
		resp, err := http.DefaultClient.Get("https://cdn.discordapp.com/soundboard-sounds/" + soundID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] saving file: %v\n", err)
			return err
		}

		contentType := resp.Header.Get("Content-Type")
		var extension string
		switch contentType {
		case "audio/mpeg3":
			extension = "mp3"
		default:
			extension = "ogg"
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] saving file, could not read response body: %v\n", err)
			return err
		}

		err = os.WriteFile(path.Join(soundsDir, soundName+"."+extension), data, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] saving file, could not write to disk: %v\n", err)
			return err
		}

		soundsWithOrdinal := make([]SoundboardSoundWithOrdinal, 0)
		for i, sound := range sounds {
			soundsWithOrdinal = append(soundsWithOrdinal, SoundboardSoundWithOrdinal{
				ordinal:         i,
				SoundboardSound: sound,
			})
		}

		if newStoredSounds, newStoredSoundMap, err := fetchStoredSounds(); err == nil {
			storedSounds = newStoredSounds
			storedSoundMap = newStoredSoundMap
			msgUpdates <- updateStoredSounds(soundsWithOrdinal).Bytes()
		}
		return nil
	}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// upgrade to websockets
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("upgrade:", err)
			return
		}
		defer c.Close()

		msgChan := make(chan []byte, 100)
		soundsWithOrdinal := make([]SoundboardSoundWithOrdinal, 0)
		for i, sound := range sounds {
			soundsWithOrdinal = append(soundsWithOrdinal, SoundboardSoundWithOrdinal{
				ordinal:         i,
				SoundboardSound: sound,
			})
		}

		// wait until all messages have been sent to the client.
		waitChan := make(chan struct{})

		// main go routine to heartbeat and send messages to clients.
		go func() {
			for {
				timer := time.NewTimer(5 * time.Second)
				var err error
				select {
				case <-timer.C:
					err = c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(2*time.Second))
				case msg := <-msgChan:
					c.SetWriteDeadline(time.Now().Add(10 * time.Second))
					err = c.WriteMessage(websocket.TextMessage, []byte(msg))
				}

				if err != nil {
					opErr := &net.OpError{}
					if errors.Is(err, websocket.ErrCloseSent) || errors.As(err, &opErr) {
						break
					}
					fmt.Fprintf(os.Stderr, "[error] write: %v %T\n", err, err)
				}
			}
			waitChan <- struct{}{}
		}()

		msgChan <- updateStoredSounds(soundsWithOrdinal).Bytes()

		// add client to map
		mu.Lock()
		clients[c] = msgChan
		mu.Unlock()

		// notify all clients new user count
		msgUpdates <- []byte(fmt.Sprintf("<span id=user-count>%d</span>", len(clients)))

		// read messages and break on errors.
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				fmt.Printf("read error: %v\n", err)
				c.Close()
				break
			}
		}

		mu.Lock()
		delete(clients, c)
		mu.Unlock()

		close(msgChan)
		<-waitChan

		// notify all clients new user count
		msgUpdates <- []byte(fmt.Sprintf("<span id=user-count>%d</span>", len(clients)))
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.FileServer(http.Dir(".")).ServeHTTP(w, r)
	})
	http.HandleFunc("/quickplay", func(w http.ResponseWriter, r *http.Request) {
		timeoutCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(timeoutCtx)
		soundLocation := r.URL.Query().Get("soundLocation")
		ordinal := r.URL.Query().Get("ordinal")
		if soundLocation == "" || ordinal == "" {
			w.WriteHeader(400)
			return
		}

		soundId := ""
		mySounds := []SoundboardSound{}
		for _, sound := range sounds {
			if sound.Name == soundLocation {
				soundId = sound.ID
				break
			} else if sound.UserID == discordClient.userID {
				mySounds = append(mySounds, sound)
			}
		}

		if soundId != "" {
			err := discordClient.SendSoundboardSound(guildID, channelID, soundId)
			if err != nil {
				fmt.Fprintf(os.Stderr, "quickplay error: %v\n", err)
				w.WriteHeader(500)
			} else {
				msgUpdates <- []byte("<div id=\"playsound\"><script>window._playSound('" + ordinal + "', true, 'green')</script></div>")

				fmt.Fprintf(os.Stdout, "sending stored sound: %v\n", soundId)
				w.WriteHeader(202)
			}
			return
		}

		msgUpdates <- []byte("<div id=\"playsound\"><script>window._playSound('" + ordinal + "', true, 'blue', true)</script></div>")

		if len(sounds) == 8 {
			randomSound := mySounds[rand.Intn(len(mySounds))]
			fmt.Fprintf(os.Stdout, "trying to delete stored sound id: %v\n", randomSound.ID)
			err = discordClient.DeleteSoundboardSound(guildID, randomSound.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "quickplay error: %v\n", err)
				w.WriteHeader(500)
				return
			}
		}

		resp, err := addSound(discordClient, storedSoundMap, addSoundInput{
			SoundLocation: soundLocation + ".mp3",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "quickplay error: %v\n", err)
			w.WriteHeader(500)
			return
		}
		fmt.Fprintf(os.Stdout, "added new stored sound: %v\n", resp)

		err = discordClient.SendSoundboardSound(guildID, channelID, resp.SoundID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "quickplay error: %v\n", err)
			w.WriteHeader(500)
			return
		}

		msgUpdates <- []byte("<div id=\"playsound\"><script>window._playSound('" + ordinal + "', true, 'green')</script></div>")

		fmt.Fprintf(os.Stdout, "sending new stored sound: %v\n", resp.SoundID)
		w.WriteHeader(202)
	})

	// server
	go func() {
		port := "3000"
		fmt.Printf("starting http server on localhost:%s...\n", port)
		host := "0.0.0.0:"
		host = "127.0.0.1:"
		err := http.ListenAndServe(host+port, http.DefaultServeMux)
		if err != nil {
			panic(err)
		}
	}()

	// Returns true on critical error
	connectDiscordWebsocket := func() (error, bool) {
		conn, _, err := websocket.DefaultDialer.Dial("wss://gateway.discord.gg/?encoding=json&v=9", http.Header{})
		if err != nil {
			return err, true
		}
		done := make(chan struct{})
		defer func() {
			done <- struct{}{}
			conn.Close()
		}()

		// receiving messages from the discord websocket
		recvMsgChan := make(chan DiscordMessage, 100)
		go func() {
			for {
				var msg DiscordMessage
				err := conn.ReadJSON(&msg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[discord-websocket] read error occurred shutting down: %v", err)
					close(recvMsgChan)
					return
				}

				switch msg.Data.(type) {
				case map[string]any:
					data, err := json.Marshal(msg.Data)
					if err != nil {
						close(recvMsgChan)
						return
					}
					var dmd DiscordMessageData
					err = json.Unmarshal(data, &dmd)
					if err != nil {
						close(recvMsgChan)
						return
					}
					msg.Data = &dmd
				}

				recvMsgChan <- msg
			}
		}()

		err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":2,"d":{"token":"`+authToken+`","capabilities":30717,"properties":{"os":"Windows","browser":"Chrome","device":"","system_locale":"en-US","browser_user_agent":"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36","browser_version":"125.0.0.0","os_version":"10","referrer":"https://www.google.com/","referring_domain":"www.google.com","search_engine":"google","referrer_current":"","referring_domain_current":"","release_channel":"stable","client_build_number":301920,"client_event_source":null,"design_id":0},"presence":{"status":"unknown","since":0,"activities":[],"afk":false},"compress":false,"client_state":{"guild_versions":{}}}}`))
		if err != nil {
			return err, true
		}
		err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":31,"d":{"guild_ids":["`+guildID+`"]}}`))
		if err != nil {
			return err, false
		}

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		// writing messages to the discord websocket and also sending heartbeats.
		msgChan := make(chan []byte, 100)
		go func() {
			for {
				select {
				case <-done:
					return
				case msg := <-msgChan:
					err = conn.WriteMessage(websocket.TextMessage, msg)
					if err != nil {
						fmt.Fprintf(os.Stderr, "[error] writing message to discord ws %v\n", err)
					}
					ticker.Reset(10 * time.Second)
				case <-ticker.C:
					err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":1,"d":4}`))
					if err != nil {
						return
					}
				}
			}
		}()

		fetchSoundboardSounds := func() {
			msgChan <- []byte(`{"op":31,"d":{"guild_ids":["` + guildID + `"]}}`)
		}

		for recvMsg := range recvMsgChan {
			if recvMsg.Type == nil || recvMsg.Data == nil {
				continue
			}

			dmd, ok := recvMsg.Data.(*DiscordMessageData)
			if !ok {
				continue
			}

			// additional ready data
			if *recvMsg.Type == "READY_SUPPLEMENTAL" {
				for _, guild := range dmd.Guilds {
					if guild.ID != guildID {
						continue
					}
					for _, voiceState := range guild.VoiceStates {
						if voiceState.ChannelID == channelID {
							userIsInChannel.Store(true)
						}
					}
				}
			} else if *recvMsg.Type == "READY" {
				for _, user := range dmd.Users {
					if user.Avatar != "" {
						userInfoCache[user.ID] = UserInfo{
							UserID:   user.ID,
							Avatar:   user.Avatar,
							Username: user.Username,
						}
					}
				}
			} else if *recvMsg.Type == "SOUNDBOARD_SOUNDS" && dmd.GuildID == guildID {
				newSounds := [soundboardSoundCount]SoundboardSound{}

				for i, soundboardSound := range dmd.SoundboardSounds {
					id := soundboardSound.SoundID
					name := soundboardSound.Name

					userID := soundboardSound.UserID
					if soundboardSound.User.Avatar != "" {
						avatar := soundboardSound.User.Avatar
						old := userInfoCache[userID]
						old.Avatar = avatar
						userInfoCache[userID] = old
					}
					newSound := SoundboardSound{Name: name, ID: id, UserID: userID, Avatar: soundboardSound.User.Avatar}
					if newSound != (SoundboardSound{}) {
						// if we detect a new sound that we don't have try to save it.
						if _, ok := storedSoundMap[newSound.Name]; !ok {
							saveSoundFunc(newSound.ID, newSound.Name)
						}
					}
					newSounds[i] = newSound
				}
				sounds = newSounds
			} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_CREATE" { // someone added to the soundboard
				json.NewEncoder(os.Stdout).Encode(recvMsg)
				fetchSoundboardSounds()
			} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_DELETE" { // someone removed from the soundboard
				json.NewEncoder(os.Stdout).Encode(recvMsg.Data)
				fetchSoundboardSounds()
			} else if *recvMsg.Type == "VOICE_STATE_UPDATE" {
				updateUserID := dmd.UserID
				updateGuildID := dmd.GuildID
				if updateUserID == discordClient.userID && guildID == updateGuildID {
					updateChannelID := dmd.ChannelID
					userIsInChannel.Store(updateChannelID == channelID)
				}
				var playSoundPayload []byte
				if userIsInChannel.Load() {
					playSoundPayload = []byte("<div id=\"playsound\"><script>document.querySelectorAll('button.send-sound-btn').forEach(btn => btn.removeAttribute('disabled'))</script></div>")
				} else {
					playSoundPayload = []byte("<div id=\"playsound\"><script>document.querySelectorAll('button.send-sound-btn').forEach(btn => btn.setAttribute('disabled', true))</script></div>")
				}

				msgUpdates <- playSoundPayload
			}
		}
		return nil, false
	}

	for {
		for i := 0; i < 5; i++ {
			err, isCritical := connectDiscordWebsocket()
			fmt.Fprintf(os.Stderr, "error occurred with discord's websocket: %v", err)
			if !isCritical {
				continue
			}
			panic(err)
		}
	}
}

func init() {
	authToken = os.Getenv("AUTH_TOKEN")
	soundsDir = os.Getenv("SOUNDS_DIR")
}
