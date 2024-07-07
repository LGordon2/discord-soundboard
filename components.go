package main

import (
	"strings"
	"text/template"
)

var (
	addSoundCardComponentTmpl *template.Template
	soundCardComponentTmpl    *template.Template
)

const addSoundCardComponentTmplRaw = `
    <div hx-on="htmx:afterProcessNode: window._updateOrder(this)" data-soundname="{{ .soundName }}"
        class="h-12 min-w-72 max-w-sm p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700">
        <div class="flex flex-row">
            <h5 class="flex-1 max-w-60 font-bold text-xl truncate text-gray-900 dark:text-white">{{ .soundName }}
            </h5>
            <button class="flex shrink items-center justify-center enabled:text-green-500 disabled:text-gray-500" hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'plus')" hx-on:click="window._updateStats('{{ .soundName }}')" hx-post="/add-sound?soundLocation={{ .soundName }}&guildID={{ .guildID }}" {{if .disabled}}disabled="true"{{end}}></button>
        </div>
    </div>
`

const soundCardComponentTmplRaw = `
<div id="soundboard-{{.ordinal}}"
            class="h-24 w-72 p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700">
            {{ if .used }}
            <div class="flex flex-row">
                <h5 class="flex-1 mb-2 text-xl font-bold tracking-tight text-gray-900 dark:text-white truncate">{{.soundName}}
                </h5>
                <div class="shrink mb-2">
                    <button hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'save')" class="enabled:text-blue-500 disabled:text-gray-500"
                        hx-post="/save-sound?soundID={{.soundId}}&soundName={{.soundName}}" {{if not .canSave}}disabled="true"{{end}}>
                    </button>
                </div>
                <a class="shrink text-blue-500 ml-1"
                    href="https://cdn.discordapp.com/soundboard-sounds/{{.soundId}}" hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'download'); new Audio(this.href)"></a>
            </div>

            <div class="flex flex-row divide-x divide-gray-700">
                <button hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'headphones')" hx-on:click="window._playSound('soundboard-{{.ordinal}}', '{{.soundId}}')" class="flex flex-1 items-center justify-center mt-1"></button>
                <button hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'play')" hx-post="/send-sound?soundID={{.soundId}}" hx-swap="none" hx-on:click="window._playSound('soundboard-{{.ordinal}}', '{{.soundId}}')" class="flex flex-1 items-center justify-center mt-1 enabled:text-green-500 disabled:text-gray-500" {{if not .canSend}}disabled="true"{{end}}></button>
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

func soundCardComponent(i int, id, name string, canSend bool, canSave, deleteButton any) string {
	var builder strings.Builder
	m := map[string]any{
		"ordinal":      i,
		"soundId":      id,
		"soundName":    name,
		"deleteButton": deleteButton,
		"canSend":      canSend,
		"canSave":      canSave,
		"used":         id != "" && name != "" && deleteButton != nil,
	}
	err := soundCardComponentTmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func addSoundCardComponent(storedSound, guildID string, disabled bool) string {
	var builder strings.Builder
	m := map[string]any{
		"soundName": storedSound,
		"guildID":   guildID,
		"disabled":  disabled,
	}
	err := addSoundCardComponentTmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func init() {
	addSoundCardComponentTmpl = template.Must(template.New("addSoundCardComponentTmpl").Parse(addSoundCardComponentTmplRaw))
	soundCardComponentTmpl = template.Must(template.New("soundCardComponentTmpl").Parse(soundCardComponentTmplRaw))
}
