package main

import (
	"encoding/base64"
	"strings"
	"text/template"
)

var (
	soundCardComponentTmpl *template.Template
)

const soundCardComponentTmplRaw = `
<div id="soundboard-{{.ordinal}}" class="h-24 w-72 p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700">
		<div class="flex flex-row">
			<h5 class="flex-1 mb-2 text-xl font-bold tracking-tight text-gray-900 dark:text-white truncate">{{.soundName}}</h5>
			<a
				id="soundboard-{{.ordinal}}-download-link"
				class="shrink text-blue-500 ml-1"
				download="{{.soundName}}.mp3"
				href="data:audio/mp3;base64,{{.soundData}}"
				hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'download')">
			</a>
		</div>

		<div class="flex flex-row divide-x divide-gray-700">
			<button
				hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'headphones')"
				hx-on:click="window._playSound('{{.ordinal}}', false, 'green')"
				class="flex flex-1 items-center justify-center mt-1">
			</button>
			<button
				id="send-sound-btn-{{.ordinal}}"
				hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'play')"
				hx-post="/quickplay?soundLocation={{.soundName}}&ordinal={{.ordinal}}"
				
				hx-swap="none"
				class="send-sound-btn flex flex-1 items-center justify-center mt-1 enabled:text-green-500 disabled:text-gray-500"
				{{if not .canSend}}disabled="true"{{end}}>
			</button>
		</div>
	</div>
`

func soundCardComponent(ordinal int, storedSound, guildID string, canSend bool, onSoundboard bool, soundData []byte) string {
	var builder strings.Builder
	m := map[string]any{
		"ordinal":      ordinal,
		"soundName":    storedSound,
		"guildID":      guildID,
		"canSend":      canSend,
		"soundData":    base64.StdEncoding.EncodeToString(soundData),
		"onSoundboard": onSoundboard,
	}
	err := soundCardComponentTmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func init() {
	soundCardComponentTmpl = template.Must(template.New("soundCardComponentTmpl").Parse(soundCardComponentTmplRaw))
}
