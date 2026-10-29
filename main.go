package main

import (
	"bytes"
	"fmt"
	htmlEscape "html"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
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
	clientID     string // these are TODO, but unused.
	clientSecret string
	authToken    string // grab from a discord API call
	soundsDir    string // where you store sounds on the server (e.g. /home/user/sounds/...)
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

func deleteButton(soundId, guildId, username, avatarCDN string, disabled bool) string {
	textColor := "text-rose-400"
	disabledProp := ""
	hiddenTooltip := ""
	if disabled {
		disabledProp = "disabled"
		textColor = "text-gray-400"
		hiddenTooltip = uploadedByComponent(username, avatarCDN)
	}

	return fmt.Sprintf(`<button data-action="delete" data-icon="minus" class="flex flex-1 peer items-center justify-center mt-1 %s" data-soundid="%s" data-guildid="%s" %s></button>%s`, textColor, soundId, guildId, disabledProp, hiddenTooltip)
}

func fetchStoredSounds() ([]string, storedSoundMap, error) {
	files, err := os.ReadDir(soundsDir)
	if err != nil {
		panic(err)
	}
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name()) < strings.ToLower(files[j].Name()) })

	storedSounds := []string{}
	cachedSounds := make(storedSoundMap)
	for _, f := range files {
		ext := path.Ext(f.Name())
		if ext != ".ogg" && ext != ".mp3" {
			continue
		}
		nameWithoutExt := strings.TrimSuffix(f.Name(), ext)
		storedSounds = append(storedSounds, f.Name())
		data, err := os.ReadFile(path.Join(soundsDir, f.Name()))
		if err == nil && len(data) > 0 {
			mimeType := "audio/ogg"
			if ext == ".mp3" {
				mimeType = "audio/mpeg"
			}
			cachedSounds[f.Name()] = prepareSoundboardSound(nameWithoutExt, mimeType, data)
		} else {
			fmt.Printf("[warn] couldn't prefetch file %s\n", f.Name())
		}
	}
	return storedSounds, cachedSounds, nil
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
	var mu, soundsMu sync.RWMutex
	sounds := [soundboardSoundCount]SoundboardSound{}
	storedSounds, storedSoundMap, err := fetchStoredSounds()
	if err != nil {
		panic(err)
	}
	discordClient := NewDiscordRestClient(authToken, "")

	msgUpdates := make(chan []byte, 100)
	soundUpdates := make(chan []SoundboardSoundWithOrdinal, 100)
	clients := make(map[*websocket.Conn]chan []byte)
	latestSoundUpdate := func(newSounds []SoundboardSoundWithOrdinal) bytes.Buffer {
		var buf bytes.Buffer
		// write updates for new sounds
		for _, sound := range newSounds {
			if sound.SoundboardSound == (SoundboardSound{}) {
				buf.WriteString(soundCardComponent(sound.ordinal, "", "", userIsInChannel.Load(), false, true, nil))
				continue
			}
			disabled := sound.UserID != discordClient.userID
			cannotSave := storedSoundMap.containsName(sound.Name)
			userInfo := userInfoCache[sound.UserID]
			avatarCDN := fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.webp", sound.UserID, userInfo.Avatar)
			buf.WriteString(soundCardComponent(sound.ordinal, sound.ID, sound.Name, userIsInChannel.Load(), !cannotSave, !disabled, deleteButton(sound.ID, guildID, userInfo.Username, avatarCDN, disabled)))
		}

		hasEmpty := false
		hiddenSounds := make([]string, 0)
		// This is used later to prune sounds that can be added or disables adding new sounds.
		soundsMu.RLock()
		for _, sound := range sounds {
			if sound == (SoundboardSound{}) {
				hasEmpty = true
				continue
			}

			encodedName, _ := json.Marshal(sound.Name)
			hiddenSounds = append(hiddenSounds, string(encodedName))
		}
		soundsMu.RUnlock()
		hiddenSoundString := "[" + strings.Join(hiddenSounds, ",") + "]"
		buf.WriteString(fmt.Sprintf(`<div id="addsoundscript" data-hidden-sounds="%s" data-has-empty="%t"></div>`, htmlEscape.EscapeString(hiddenSoundString), hasEmpty))

		var minifiedBuf bytes.Buffer
		m.Minify("text/html", &minifiedBuf, &buf)
		return minifiedBuf
	}
	updateStoredSounds := func(soundsWithOrdinal []SoundboardSoundWithOrdinal) *bytes.Buffer {
		var buf bytes.Buffer = latestSoundUpdate(soundsWithOrdinal)
		soundMap := make(map[string]bool)
		hasEmpty := false
		// This is used later to prune sounds that can be added or disables adding new sounds.
		soundsMu.RLock()
		for _, sound := range sounds {
			if sound == (SoundboardSound{}) {
				hasEmpty = true
				continue
			}
			soundMap[sound.Name] = true
		}
		soundsMu.RUnlock()
		_ = hasEmpty

		buf.WriteString("<div id=\"storedsounds\" class=\"flex flex-1 flex-wrap justify-center items-center max-w-7xl\">")
		for _, storedSound := range storedSounds {
			ext := filepath.Ext(storedSound)
			storedSoundNoExt := strings.TrimSuffix(storedSound, ext)
			// hide sounds already present on the sound map
			_, ok := soundMap[storedSoundNoExt]
			buf.WriteString(addSoundCardComponent(storedSoundNoExt, ext, guildID, ok))
		}
		buf.WriteString("</div>")
		return &buf
	}

	go func() {
		for newSounds := range soundUpdates {
			buf := latestSoundUpdate(newSounds)
			msgUpdates <- buf.Bytes()
		}
	}()
	go func() {
		for msgUpdate := range msgUpdates {
			mu.RLock()
			for _, c := range clients {
				select {
				case c <- msgUpdate:
					// message sent successfully
				default:
					// channel full, drop message
					// Client will get next update anyway
				}
			}
			mu.RUnlock()
		}
	}()
	http.HandleFunc("/send-sound", func(w http.ResponseWriter, r *http.Request) {
		soundID := r.URL.Query().Get("soundID")
		if soundID == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		err := discordClient.SendSoundboardSound(guildID, channelID, soundID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] send soundboard err: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		playSoundPayload := []byte("<div id=\"playsound\" data-play-sound=\"" + htmlEscape.EscapeString(soundID) + "\"></div>")
		mu.RLock()
		for _, clientChan := range clients {
			select {
			case clientChan <- playSoundPayload:
			default:
				// client is slow, skip this update
			}
		}
		mu.RUnlock()
	})
	saveSoundFunc := func(soundID, soundName string) error {
		resp, err := http.DefaultClient.Get("https://cdn.discordapp.com/soundboard-sounds/" + soundID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] saving file: %v\n", err)
			return err
		}

		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("downloading sound: HTTP %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		var extension string
		switch contentType {
		case "audio/mpeg", "audio/mp3", "audio/mpeg3":
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

		soundboardSound := SoundboardSoundWithOrdinal{}
		soundsWithOrdinal := make([]SoundboardSoundWithOrdinal, 0)
		soundsMu.RLock()
		for i, sound := range sounds {
			if sound.ID == soundID {
				soundboardSound = SoundboardSoundWithOrdinal{
					ordinal:         i,
					SoundboardSound: sound,
				}
			}
			soundsWithOrdinal = append(soundsWithOrdinal, SoundboardSoundWithOrdinal{
				ordinal:         i,
				SoundboardSound: sound,
			})
		}
		soundsMu.RUnlock()

		if newStoredSounds, newStoredSoundMap, err := fetchStoredSounds(); err == nil {
			storedSounds = newStoredSounds
			storedSoundMap = newStoredSoundMap
			if soundboardSound != (SoundboardSoundWithOrdinal{}) {
				soundUpdates <- []SoundboardSoundWithOrdinal{
					soundboardSound,
				}
			}
			msgUpdates <- updateStoredSounds(soundsWithOrdinal).Bytes()
		}

		return nil
	}

	http.HandleFunc("/save-sound", func(w http.ResponseWriter, r *http.Request) {
		soundID := r.URL.Query().Get("soundID")
		soundName := r.URL.Query().Get("soundName")
		if soundID == "" || soundName == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := saveSoundFunc(soundID, soundName); err != nil {
			fmt.Fprintf(os.Stderr, "[error] saving file: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("upgrade:", err)
			return
		}
		defer c.Close()
		soundChan := make(chan []byte, 100)
		soundsWithOrdinal := make([]SoundboardSoundWithOrdinal, 0)
		soundsMu.RLock()
		for i, sound := range sounds {
			soundsWithOrdinal = append(soundsWithOrdinal, SoundboardSoundWithOrdinal{
				ordinal:         i,
				SoundboardSound: sound,
			})
		}
		soundsMu.RUnlock()

		waitChan := make(chan struct{})

		go func() {
			defer close(waitChan)
			defer c.Close()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			for {
				var err error
				select {
				case <-ticker.C:
					err = c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(2*time.Second))
				case sound, ok := <-soundChan:
					if !ok {
						return
					}
					c.SetWriteDeadline(time.Now().Add(10 * time.Second))
					err = c.WriteMessage(websocket.TextMessage, []byte(sound))
				}

				if err != nil {
					return
				}
			}
		}()
		var buf bytes.Buffer
		buf.WriteString("<div id=\"playable-sounds\" class=\"flex flex-1 flex-wrap justify-center items-center max-w-7xl md:sticky md:top-0 md:bg-white md:dark:bg-gray-900\">")
		for i := 0; i < soundboardSoundCount; i++ {
			buf.WriteString(fmt.Sprintf("<div id=\"soundboard-%d\"></div>", i))
		}
		buf.WriteString("</div>")
		soundChan <- buf.Bytes()

		soundChan <- updateStoredSounds(soundsWithOrdinal).Bytes()

		mu.Lock()
		clients[c] = soundChan
		clientCount := len(clients)
		mu.Unlock()

		msgUpdates <- []byte(fmt.Sprintf("<span id=user-count>%d</span>", clientCount))

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
		clientCount = len(clients)
		mu.Unlock()

		close(soundChan)
		<-waitChan

		msgUpdates <- []byte(fmt.Sprintf("<span id=user-count>%d</span>", clientCount))
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")

		if code != "" {
			var buf bytes.Buffer
			buf.WriteString("grant_type=authorization_code&code=" + code + "&redirect_uri=http://localhost:3000")
			req, err := http.NewRequest(http.MethodPost, "https://discord.com/api/oauth2/token", &buf)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "[error] %v", err)
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetBasicAuth(clientID, clientSecret)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "[error] %v", err)
				return
			}
			if resp.StatusCode != http.StatusOK {
				data, _ := io.ReadAll(resp.Body)
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "[error] invalid status code %v %v", resp.StatusCode, string(data))
				return
			}

			var m map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&m)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "[error] %v", err)
				return
			}

			fmt.Println(m)
			discordClient = NewDiscordRestClient(m["access_token"].(string), "Bearer")
			r2, err := http.NewRequest("GET", "/", nil)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "[error] %v", err)
				return
			}
			http.Redirect(w, r2, "/", http.StatusFound)
			return
		}

		http.FileServer(http.Dir("./dist")).ServeHTTP(w, r)
	})
	http.HandleFunc("/swap-sound", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() { log.Printf("swap-sound total: %v", time.Since(start)) }()
		input := swapSoundInput{}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := swapSound(discordClient, storedSoundMap, input); err != nil {
			log.Printf("[error] swapping sound: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})
	http.Handle("/delete-sound", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := deleteSound(discordClient, r.URL.Query().Get("guildID"), deleteSoundInput{
			SoundID: r.URL.Query().Get("soundID"),
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%v", err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	http.HandleFunc("/add-sound", func(w http.ResponseWriter, r *http.Request) {
		soundLocation := r.URL.Query().Get("soundLocation")
		err := addSound(discordClient, storedSoundMap, addSoundInput{
			SoundLocation: soundLocation,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(os.Stdout, "%v\n", err)
			fmt.Fprintf(w, "%v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
	})
	http.Handle("/sounds", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.WriteString("<ul>")
		soundsMu.RLock()
		for _, sound := range sounds {
			buf.WriteString(fmt.Sprintf("<li>%s (%s) <button onclick=\"new Audio('https://cdn.discordapp.com/soundboard-sounds/%s').play()\">Play</button><button data-action=\"delete\" data-soundid=\"%s\" data-guildid=\"%s\">Delete</button></li>", sound.Name, sound.ID, sound.ID, sound.ID, guildID))
		}
		soundsMu.RUnlock()
		for _, storedSound := range storedSounds {
			buf.WriteString(fmt.Sprintf("<li class=\"add-sound-component\" data-soundname=\"%s\" data-soundext=\"%s\">%s <button data-action=\"add\">Add</button></li>", htmlEscape.EscapeString(strings.TrimSuffix(storedSound, path.Ext(storedSound))), path.Ext(storedSound), htmlEscape.EscapeString(storedSound)))
		}
		buf.WriteString("</ul>")
		w.Write(buf.Bytes())
	}))
	http.HandleFunc("/quickplay", func(w http.ResponseWriter, r *http.Request) {
		soundId := ""
		soundsMu.RLock()
		for _, sound := range sounds {
			if sound.Name == "NoOneHeard" {
				soundId = sound.ID
				break
			}
		}
		soundsMu.RUnlock()
		if soundId == "" {
			fmt.Fprintf(os.Stderr, "could not find NoOneHeard sound replacement\n")
			w.WriteHeader(500)
			return
		}

		err := discordClient.DeleteSoundboardSound(guildID, soundId) // there is no bathroom
		if err != nil {
			panic(err)
		}

		soundLocation := r.URL.Query().Get("soundLocation")
		data, err := os.ReadFile("/Users/lew/repos/discord-soundboard/sounds/" + soundLocation)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] trouble reading file %s\n", soundLocation)
			return
		}

		arr := strings.Split(soundLocation, "/")
		nameAndExt := arr[len(arr)-1]

		extension := strings.TrimPrefix(filepath.Ext(nameAndExt), ".")
		name := strings.TrimSuffix(nameAndExt, extension)

		soundboardResponse, err := discordClient.CreateSoundboardSound(guildID, name, "audio/"+extension, data)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] creating soundboard sound for %s %v\n", soundLocation, err)
			return
		}

		err = discordClient.SendSoundboardSound(guildID, channelID, soundboardResponse.SoundID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] send soundboard sound for %s %v\n", soundboardResponse.SoundID, err)
			return
		}

		err = discordClient.DeleteSoundboardSound(guildID, soundboardResponse.SoundID) // there is no bathroom
		if err != nil {
			panic(err)
		}

		data, err = os.ReadFile("/Users/lew/repos/discord-soundboard/sounds/NoOneHeard.ogg")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] trouble reading file %s\n", soundLocation)
			return
		}

		_, err = discordClient.CreateSoundboardSound(guildID, "NoOneHeard", "audio/ogg", data)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] creating soundboard sound for %s %v\n", soundLocation, err)
			return
		}

		w.Write([]byte(fmt.Sprintf("<script type=\"text/javascript\">new Audio('http://localhost:3000/sounds/%s.%s').play();</script>", name, extension)))
	})
	go func() {
		port := "3000"
		fmt.Printf("starting http server on localhost:%s...\n", port)
		err := http.ListenAndServe("0.0.0.0:"+port, http.DefaultServeMux)
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
				case map[string]interface{}:
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
				incoming := make([]SoundboardSound, 0, len(dmd.SoundboardSounds))
				for _, sound := range dmd.SoundboardSounds {
					if sound.User.Avatar != "" {
						old := userInfoCache[sound.UserID]
						old.Avatar = sound.User.Avatar
						userInfoCache[sound.UserID] = old
					}
					incoming = append(incoming, SoundboardSound{Name: sound.Name, ID: sound.SoundID, UserID: sound.UserID, Avatar: sound.User.Avatar})
				}
				soundsMu.Lock()
				newSounds, newUpdates := reconcileSounds(sounds, incoming)
				sounds = newSounds
				soundsMu.Unlock()
				// Publish readiness before optional CDN downloads and library rescans.
				soundUpdates <- newUpdates
				for _, sound := range newSounds {
					if sound.ID != "" && !storedSoundMap.containsName(sound.Name) {
						fmt.Printf("attempting to save new sound %v\n", sound.Name)
						saveSoundFunc(sound.ID, sound.Name)
					}
				}
			} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_CREATE" {
				json.NewEncoder(os.Stdout).Encode(recvMsg)
				fetchSoundboardSounds()
			} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_DELETE" {
				json.NewEncoder(os.Stdout).Encode(recvMsg)
				fetchSoundboardSounds()
			} else if *recvMsg.Type == "VOICE_STATE_UPDATE" {
				updateUserID := dmd.UserID
				updateGuildID := dmd.GuildID
				if updateUserID == discordClient.userID && guildID == updateGuildID {
					updateChannelID := dmd.ChannelID
					userIsInChannel.Store(updateChannelID == channelID)
				}
				// just force updates on all the sounds!
				updates := make([]SoundboardSoundWithOrdinal, 0)
				for i, sound := range sounds {
					updates = append(updates, SoundboardSoundWithOrdinal{
						ordinal:         i,
						SoundboardSound: sound,
					})
				}
				soundUpdates <- updates
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
	clientID = os.Getenv("CLIENT_ID")
	clientSecret = os.Getenv("CLIENT_SECRET")
	authToken = os.Getenv("AUTH_TOKEN")
	soundsDir = os.Getenv("SOUNDS_DIR")
}
