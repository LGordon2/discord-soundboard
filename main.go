package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type DiscordMessage struct {
	Type    *string     `json:"t"`
	GuildID *string     `json:"guild_id"`
	Data    interface{} `json:"d"`
}

type SoundboardSound struct {
	Name   string
	ID     string
	UserID string
}

var (
	clientID     string // these are TODO, but unused.
	clientSecret string
	authToken    string // grab from a discord API call
	soundsDir    string // where you store sounds on the server (e.g. /home/user/sounds/...)
)

const guildID = "284709094588284929"   // Viznet
const channelID = "284709094588284930" // general channel
// const guildID = "752332599631806505" // Faceclub
// const channelID = "752332599631806509" // general channel

var upgrader = websocket.Upgrader{
	ReadBufferSize:  20 * 1024,
	WriteBufferSize: 20 * 1024,
} // use default options

func deleteButton(soundId, guildId string, disabled bool) string {
	textColor := "text-rose-400"
	disabledProp := ""
	if disabled {
		disabledProp = "disabled"
		textColor = "text-gray-400"
	}
	minusSvg := `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
	<path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14" />
  </svg>
  `

	return fmt.Sprintf(`<button class="flex flex-1 items-center justify-center mt-1 %s" hx-delete="/delete-sound?soundID=%s&guildID=%s" %s>%s</button>`, textColor, soundId, guildId, disabledProp, minusSvg)
}

func main() {
	files, err := os.ReadDir("./sounds")
	if err != nil {
		panic(err)
	}
	var userIsInChannel atomic.Bool
	userIsInChannel.Store(false)
	var mu sync.RWMutex
	sounds := make([]SoundboardSound, 0)
	storedSounds := []string{}
	for _, f := range files {
		if !(strings.HasSuffix(f.Name(), ".ogg") || strings.HasSuffix(f.Name(), ".mp3")) {
			continue
		}
		storedSounds = append(storedSounds, f.Name())
	}
	discordClient := NewDiscordRestClient(authToken, "")

	soundUpdates := make(chan struct{})
	http.HandleFunc("/component-test", func(w http.ResponseWriter, r *http.Request) {
		// components := ""
		// sounds := [][2]string{
		// 	{"1210397634343346277", "Wow"},
		// 	{"1258261093541875723", "ThatsWeird"},
		// }
		// for _, sound := range sounds {
		// 	// components += soundCardComponent(sound[0], sound[1], false)
		// }
		// w.Write([]byte(components))
	})
	clients := make(map[*websocket.Conn]chan []byte)
	go func() {
		for range soundUpdates {
			if len(sounds) == 0 {
				continue
			}
			var buf bytes.Buffer
			buf.WriteString("<div id=\"sounds\" class=\"flex flex-col justify-center items-center\">")
			i := 0
			buf.WriteString("<div class=\"flex flex-1 flex-wrap justify-center items-center\">")
			for _, sound := range sounds {
				disabled := sound.UserID != discordClient.userID
				buf.WriteString(soundCardComponent(sound.ID, sound.Name, userIsInChannel.Load(), deleteButton(sound.ID, guildID, disabled)))
				i++
			}
			for i < 8 {
				buf.WriteString(soundCardComponent("", "", userIsInChannel.Load(), nil))
				i++
			}
			buf.WriteString("</div>")
			buf.WriteString("<div class=\"flex flex-1 flex-wrap justify-center items-center\">")
			for _, storedSound := range storedSounds {
				buf.WriteString(addSoundCardComponent(storedSound, guildID))
			}
			buf.WriteString("</div>")
			buf.WriteString("</div>")
			fmt.Printf("client count: %d\n", len(clients))
			mu.RLock()
			for _, c := range clients {
				c <- buf.Bytes()
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
		err = discordClient.SendSoundboardSound(guildID, channelID, soundID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] send soundboard err: %v\n", err)
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

		mu.Lock()
		clients[c] = soundChan
		mu.Unlock()

		go func() {
			for sound := range soundChan {
				c.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := c.WriteMessage(websocket.TextMessage, []byte(sound)); err != nil {
					opErr := &net.OpError{}
					if errors.Is(err, websocket.ErrCloseSent) || errors.As(err, &opErr) {
						mu.Lock()
						delete(clients, c)
						mu.Unlock()
						return
					}
					fmt.Fprintf(os.Stderr, "[error] write: %v %T\n", err, err)
				}
			}
		}()
		soundUpdates <- struct{}{}

		waitChan := make(chan struct{})
		<-waitChan
		close(soundChan)
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
				fmt.Fprintf(w, "invalid status code %v %v", resp.StatusCode, string(data))
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
		}

		http.FileServer(http.Dir(".")).ServeHTTP(w, r)
	})
	http.Handle("/delete-sound", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := discordClient.DeleteSoundboardSound(r.URL.Query().Get("guildID"), r.URL.Query().Get("soundID"))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] deleting file %v", err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	http.HandleFunc("/add-sound", func(w http.ResponseWriter, r *http.Request) {
		soundLocation := r.URL.Query().Get("soundLocation")
		data, err := os.ReadFile(path.Join(soundsDir, soundLocation))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] trouble reading file %s", soundLocation)
			return
		}

		arr := strings.Split(soundLocation, "/")
		nameAndExt := arr[len(arr)-1]

		arr = strings.Split(nameAndExt, ".")
		name := arr[0]
		extension := arr[len(arr)-1]

		_, err = discordClient.CreateSoundboardSound(guildID, name, "audio/"+extension, data)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "[error] creating soundboard sound for %s %v", soundLocation, err)
			return
		}

		w.WriteHeader(http.StatusOK)
	})
	http.Handle("/sounds", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.WriteString("<ul>")
		for _, sound := range sounds {
			buf.WriteString(fmt.Sprintf("<li>%s (%s) <button onclick=\"new Audio('https://cdn.discordapp.com/soundboard-sounds/%s').play()\">Play</button><button hx-delete=\"/delete-sound?soundID=%s&guildID=%s\">Delete</button></li>", sound.Name, sound.ID, sound.ID, sound.ID, guildID))
		}
		for _, storedSound := range storedSounds {
			buf.WriteString(fmt.Sprintf("<li>%s <button hx-post=\"/add-sound?soundLocation=%s&guildID=%s\">Add</button></li>", storedSound, storedSound, guildID))
		}
		buf.WriteString("</ul>")
		w.Write(buf.Bytes())
	}))
	http.HandleFunc("/quickplay", func(w http.ResponseWriter, r *http.Request) {
		soundId := ""
		for _, sound := range sounds {
			if sound.Name == "NoOneHeard" {
				soundId = sound.ID
				break
			}
		}
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

		arr = strings.Split(nameAndExt, ".")
		name := arr[0]
		extension := arr[len(arr)-1]

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
		fmt.Println("starting http server on localhost:3000...")
		err := http.ListenAndServe("0.0.0.0:3000", http.DefaultServeMux)
		if err != nil {
			panic(err)
		}
	}()

	conn, _, err := websocket.DefaultDialer.Dial("wss://gateway.discord.gg/?encoding=json&v=9", http.Header{})
	if err != nil {
		panic(err)
	}

	recvMsgChan := make(chan DiscordMessage, 100)

	go func() {
		for {
			var msg DiscordMessage
			err := conn.ReadJSON(&msg)
			if err != nil {
				panic(err)
			}
			recvMsgChan <- msg
		}
	}()

	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":2,"d":{"token":"`+authToken+`","capabilities":30717,"properties":{"os":"Windows","browser":"Chrome","device":"","system_locale":"en-US","browser_user_agent":"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36","browser_version":"125.0.0.0","os_version":"10","referrer":"https://www.google.com/","referring_domain":"www.google.com","search_engine":"google","referrer_current":"","referring_domain_current":"","release_channel":"stable","client_build_number":301920,"client_event_source":null,"design_id":0},"presence":{"status":"unknown","since":0,"activities":[],"afk":false},"compress":false,"client_state":{"guild_versions":{}}}}`))
	if err != nil {
		panic(err)
	}
	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":31,"d":{"guild_ids":["`+guildID+`"]}}`))
	if err != nil {
		panic(err)
	}

	go func() {
		t := time.NewTicker(10 * time.Second)
		for range t.C {
			err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":1,"d":4}`))
			if err != nil {
				panic(err)
			}
		}
	}()

	for recvMsg := range recvMsgChan {
		if recvMsg.Type == nil || recvMsg.Data == nil {
			continue
		}

		fetchSoundboardSounds := func() {
			err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":31,"d":{"guild_ids":["`+guildID+`"]}}`))
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] fetch soundboard sounds error %v\n", err)
			}
		}

		if *recvMsg.Type == "SOUNDBOARD_SOUNDS" && recvMsg.Data.(map[string]interface{})["guild_id"] == guildID {
			newSounds := make([]SoundboardSound, 0)
			for _, soundboardSound := range recvMsg.Data.(map[string]interface{})["soundboard_sounds"].([]interface{}) {
				name := soundboardSound.(map[string]interface{})["name"].(string)
				id := soundboardSound.(map[string]interface{})["sound_id"].(string)
				userID := soundboardSound.(map[string]interface{})["user_id"].(string)
				newSounds = append(newSounds, SoundboardSound{Name: name, ID: id, UserID: userID})
			}
			sounds = newSounds
			soundUpdates <- struct{}{}
		} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_CREATE" {
			fetchSoundboardSounds()
		} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_DELETE" {
			json.NewEncoder(os.Stdout).Encode(recvMsg)
			fetchSoundboardSounds()
		} else if *recvMsg.Type == "VOICE_STATE_UPDATE" {
			updateUserID := recvMsg.Data.(map[string]any)["user_id"].(string)
			updateGuildID := recvMsg.Data.(map[string]any)["guild_id"].(string)
			if updateUserID == discordClient.userID && guildID == updateGuildID {
				updateChannelID, ok := recvMsg.Data.(map[string]any)["channel_id"].(string)
				userIsInChannel.Store(ok && updateChannelID == channelID)
			}
			fetchSoundboardSounds()
		}
	}
}

func init() {
	clientID = os.Getenv("CLIENT_ID")
	clientSecret = os.Getenv("CLIENT_SECRET")
	authToken = os.Getenv("AUTH_TOKEN")
	soundsDir = os.Getenv("SOUNDS_DIR")
}
