package main

import (
	"bytes"
	"text/template"
)

const addSoundCardComponentTmpl = `
    <div hx-on="htmx:afterProcessNode: window._updateOrder(this)" data-soundname="{{ .soundName }}"
        class="h-12 min-w-72 max-w-sm p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700">
        <div class="flex flex-row">
            <h5 class="flex-1 max-w-60 font-bold text-xl truncate text-gray-900 dark:text-white">{{ .soundName }}
            </h5>
            <button class="flex shrink items-center justify-center enabled:text-green-500 disabled:text-gray-500" hx-on:click="window._updateStats('{{ .soundName }}')" hx-post="/add-sound?soundLocation={{ .soundName }}&guildID={{ .guildID }}" {{if .disabled}}disabled="true"{{end}}><svg class="h-8 w-8"
                    viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
                    stroke-linejoin="round">
                    <line x1="12" y1="5" x2="12" y2="19"></line>
                    <line x1="5" y1="12" x2="19" y2="12"></line>
                </svg></button>
        </div>
    </div>
`

const soundCardComponentTmpl = `
<div id="box-{{.soundId}}"
            class="h-24 w-72 p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700">
            {{ if .used }}
            <div class="flex flex-row">
                <h5 class="flex-1 mb-2 text-xl font-bold tracking-tight text-gray-900 dark:text-white truncate">{{.soundName}}
                </h5>
                <div class="shrink mb-2">
                    <button class="enabled:text-blue-500 disabled:text-gray-500"
                        hx-post="/save-sound?soundID={{.soundId}}&soundName={{.soundName}}" {{if not .canSave}}disabled="true"{{end}}><svg class="h-6 w-6"  viewBox="0 0 24 24"  fill="none"  stroke="currentColor"  stroke-width="2"  stroke-linecap="round"  stroke-linejoin="round">  <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />  <polyline points="17 21 17 13 7 13 7 21" />  <polyline points="7 3 7 8 15 8" /></svg>
                    </button>
                </div>
                <a class="shrink text-blue-500 ml-1"
                    href="https://cdn.discordapp.com/soundboard-sounds/{{.soundId}}"><svg
                        class="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                        stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                        <polyline points="7 10 12 15 17 10" />
                        <line x1="12" y1="15" x2="12" y2="3" />
                    </svg></a>
            </div>

            <div class="flex flex-row divide-x divide-gray-700">
                <button onclick="window._playSound('{{.soundId}}')" class="flex flex-1 items-center justify-center mt-1"><svg class="h-8 w-8 text-yellow-500"  width="24" height="24" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none" stroke-linecap="round" stroke-linejoin="round">  <path stroke="none" d="M0 0h24v24H0z"/>  <rect x="4" y="13" rx="2" width="5" height="7" />  <rect x="15" y="13" rx="2" width="5" height="7" />  <path d="M4 15v-3a8 8 0 0 1 16 0v3" /></svg></button>
                <button hx-post="/send-sound?soundID={{.soundId}}" hx-swap="none" hx-on:click="window._playSound('{{.soundId}}')" class="flex flex-1 items-center justify-center mt-1 enabled:text-green-500 disabled:text-gray-500" {{if not .canSend}}disabled="true"{{end}}><svg class="h-8 w-8"  viewBox="0 0 24 24"  fill="none"  stroke="currentColor"  stroke-width="2"  stroke-linecap="round"  stroke-linejoin="round">  <polygon points="5 3 19 12 5 21 5 3" /></svg></button>
                {{ if .deleteButton }}
                {{.deleteButton}}
                {{ end }}
            </div>
            {{ else }}
            <div class="flex items-center justify-center text-center">
                <h5 class="text-xl text-slate-400">Free</h5>
            </div>
            {{ end }}
        </div>
`

func soundCardComponent(id, name string, canSend bool, canSave, deleteButton any) string {
	var buf bytes.Buffer
	m := map[string]any{
		"soundId":      id,
		"soundName":    name,
		"deleteButton": deleteButton,
		"canSend":      canSend,
		"canSave":      canSave,
		"used":         id != "" && name != "" && deleteButton != nil,
	}
	err := template.Must(template.New("soundCardComponentTmpl").Parse(soundCardComponentTmpl)).Execute(&buf, m)
	if err != nil {
		panic(err)
	}
	return buf.String()
}

func addSoundCardComponent(storedSound, guildID string, disabled bool) string {
	var buf bytes.Buffer
	m := map[string]any{
		"soundName": storedSound,
		"guildID":   guildID,
		"disabled":  disabled,
	}
	err := template.Must(template.New("addSoundCardComponentTmpl").Parse(addSoundCardComponentTmpl)).Execute(&buf, m)
	if err != nil {
		panic(err)
	}
	return buf.String()
}
