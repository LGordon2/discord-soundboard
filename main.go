package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  50 * 1024,
	WriteBufferSize: 50 * 1024,
} // use default options

func playButton(soundId string) string {
	playSvg := `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
	<path stroke-linecap="round" stroke-linejoin="round" d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.347a1.125 1.125 0 0 1 0 1.972l-11.54 6.347a1.125 1.125 0 0 1-1.667-.986V5.653Z" />
  </svg>
  `
	return fmt.Sprintf(`<button onclick="window._playSound('%s')">%s</button>`, soundId, playSvg)
}

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

func soundDetail(name, id string) string {
	return fmt.Sprintf(`<span class="flex basis-3/4">%s (<a class="text-sky-400" href="https://cdn.discordapp.com/soundboard-sounds/%s">%s</a>)</span>`, name, id, id)
}

func main() {
	files, err := os.ReadDir("./sounds")
	if err != nil {
		panic(err)
	}
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
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("upgrade:", err)
			return
		}
		defer c.Close()

		for {
			var buf bytes.Buffer
			buf.WriteString("<div id=\"sounds\" class=\"flex flex-row flex-wrap justify-center\">")
			i := 0
			if len(sounds) == 0 {
				panic("no sounds found!")
			}
			newSoundsStr := ""
			for _, sound := range sounds {
				newSoundsStr += sound.Name + ", "
			}
			fmt.Printf("new sounds: %v\n", newSoundsStr)
			for _, sound := range sounds {
				disabled := sound.UserID != discordClient.userID
				// if sound.UserID != discordClient.userID {
				// 	disabled = "disabled"
				// }
				buf.WriteString(soundCardComponent(sound.ID, sound.Name, deleteButton(sound.ID, guildID, disabled)))
				// buf.WriteString(fmt.Sprintf(`<div id="box-%s" class="flex-1 shadow-2xl rounded-md border-2 py-2 px-4 mx-96 mt-2">`, sound.ID))
				// buf.WriteString(fmt.Sprintf("<div class=\"flex flex-row\">%s<div class=\"flex-1 flex flex-row-reverse\">%s%s</div></div>", soundDetail(sound.Name, sound.ID), deleteButton(sound.ID, guildID, disabled), playButton(sound.ID)))
				// buf.WriteString("</div>")
				i++
			}
			for i < 8 {
				buf.WriteString(soundCardComponent("", "", nil))
				i++
			}

			// 	plusIcon := `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
			// 	<path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
			//   </svg>
			//   `
			for _, storedSound := range storedSounds {
				buf.WriteString(addSoundCardComponent(storedSound, guildID))
				// buf.WriteString(`<div class="flex-1 shadow-2xl rounded-md border-2 py-2 px-4 mx-96 mt-2">`)
				// quickplay is disabled until I figure out ratelimiting
				// qpBtn := fmt.Sprintf(`<button class="flex-1 flex flex-row text-green-500 disabled:text-slate-400" hx-post="/quickplay?soundLocation=%s&guildID=%s" hx-target="#search-results" disabled>Quickplay</button>`, storedSound, guildID)
				// addBtn := fmt.Sprintf(`<button class="flex-1 flex flex-row text-green-500 disabled:text-slate-400" hx-post="/add-sound?soundLocation=%s&guildID=%s">%s</button>`, storedSound, guildID, plusIcon)
				// buf.WriteString(fmt.Sprintf("<div class=\"flex flex-row\"><span class=\"flex-1 basis-3/4\">%s</span><div class=\"flex flex-row-reverse\">%s</div></div>", storedSound, addBtn))
				// buf.WriteString("</div>")
			}
			buf.WriteString("</div>")
			if err := c.WriteMessage(websocket.TextMessage, buf.Bytes()); err != nil {
				fmt.Fprintf(os.Stderr, "[error] write: %v\n", err)
				break
			}
			fmt.Println("wrote message successfully")
			<-soundUpdates
			fmt.Println("found new sound updates!")
		}
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
			// json.NewEncoder(os.Stdout).Encode(recvMsg)
			newSounds := make([]SoundboardSound, 0)
			for _, soundboardSound := range recvMsg.Data.(map[string]interface{})["soundboard_sounds"].([]interface{}) {
				name := soundboardSound.(map[string]interface{})["name"].(string)
				id := soundboardSound.(map[string]interface{})["sound_id"].(string)
				userID := soundboardSound.(map[string]interface{})["user_id"].(string)
				newSounds = append(newSounds, SoundboardSound{Name: name, ID: id, UserID: userID})
			}
			sounds = newSounds
			newSoundsStr := ""
			for _, sound := range sounds {
				newSoundsStr += sound.Name + ", "
			}
			fmt.Printf("new sounds: %v\n", newSoundsStr)
			soundUpdates <- struct{}{}
		} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_CREATE" {
			fetchSoundboardSounds()
		} else if *recvMsg.Type == "GUILD_SOUNDBOARD_SOUND_DELETE" {
			json.NewEncoder(os.Stdout).Encode(recvMsg)
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
