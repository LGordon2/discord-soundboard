package main

import (
	"encoding/base64"
	"strings"
	"text/template"
)

var (
	addSoundCardComponentTmpl *template.Template
	soundCardComponentTmpl    *template.Template
	soundCardComponent2Tmpl   *template.Template
	uploadedByComponentTmpl   *template.Template
)

const uploadedByComponentTmplRaw = `
    <div class="invisible peer-hover:visible hover:visible" style="position: absolute; transform: translate(54px, 26px)">
        <div
            class="max-h-24 max-w-72 p-2 m-2 rounded-lg shadow bg-gray-700 flex flex-row text-sm font-medium text-gray-900 dark:text-white truncate">
            <span class="flex items-center mr-4">Uploaded by</span>
            <img class="rounded-full w-8 h-8"
                src="{{.avatarCDN}}?size=32">
            <span class="flex items-center ml-1 truncate">{{.username}}</span>

        </div>
        <svg style="transform: translateY(-64px) translateX(164px)" class="text-gray-700" width="10"
            height="10" xmlns="http://www.w3.org/2000/svg">
            <polygon points="5,1.5 9,8.5 1,8.5" fill="currentColor" stroke-width="2" />
        </svg>
    </div>
`

const addSoundCardComponentTmplRaw = `
    <div draggable="true" hx-on="htmx:beforeProcessNode: window._makeDraggable(this)" data-soundname="{{ .soundName }}"
        class="add-sound-component h-12 min-w-72 max-w-sm p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700 {{if .hidden}}hidden{{end}}">
        <div class="flex flex-row">
            <h5 class="flex-1 max-w-60 font-bold text-xl truncate text-gray-900 dark:text-white">{{ .soundName }}
            </h5>
            <button class="flex shrink items-center justify-center disabled:text-gray-500 text-green-500" hx-swap="none" hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'plus')" hx-post="/add-sound?soundLocation={{ .soundName }}&guildID={{ .guildID }}"></button>
			<button class="flex shrink items-center justify-center disabled:text-gray-500 text-green-500" hx-swap="none" hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'play')" hx-post="/quickplay2?soundLocation={{ .soundName }}&guildID={{ .guildID }}"></button>
        </div>
    </div>
`

const soundCardComponentTmplRaw = `
<div {{ if .canRemove }}hx-on="htmx:beforeProcessNode: window._makeDroppable(this)"{{end}} data-soundid="{{.soundId}}" id="soundboard-{{.ordinal}}"
            class="h-24 w-72 p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700 {{ if .canRemove }}droppable{{ end }}">
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
                <button hx-on="htmx:beforeProcessNode: window._iconLoad(this, 'play')" hx-post="/send-sound?soundID={{.soundId}}" hx-on:click="window._highlightSound('soundboard-{{.ordinal}}', '{{.soundId}}')" hx-swap="none" class="flex flex-1 items-center justify-center mt-1 enabled:text-green-500 disabled:text-gray-500" {{if not .canSend}}disabled="true"{{end}}></button>
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

const soundCardComponent2TmplRaw = `
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
			{{if .onSoundboard}}<span>on soundboard</span>{{end}}
		</div>
	</div>
`

// hx-on:click="window._highlightSound('{{.ordinal}}')"

func soundCardComponent(i int, id, name string, canSend, canSave, canRemove bool, deleteButton any) string {
	var builder strings.Builder
	used := id != "" && name != "" && deleteButton != nil
	m := map[string]any{
		"ordinal":      i,
		"soundId":      id,
		"soundName":    name,
		"deleteButton": deleteButton,
		"canSend":      canSend,
		"canSave":      canSave,
		"canRemove":    canRemove || !used,
		"used":         used,
	}
	err := soundCardComponentTmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func soundCardComponent2(ordinal int, storedSound, guildID string, canSend bool, onSoundboard bool, soundData []byte) string {
	var builder strings.Builder
	m := map[string]any{
		"ordinal":      ordinal,
		"soundName":    storedSound,
		"guildID":      guildID,
		"canSend":      canSend,
		"soundData":    base64.StdEncoding.EncodeToString(soundData),
		"onSoundboard": onSoundboard,
	}
	err := soundCardComponent2Tmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func addSoundCardComponent(storedSound, guildID string, disabled, hidden bool) string {
	var builder strings.Builder
	m := map[string]any{
		"soundName": storedSound,
		"guildID":   guildID,
		"disabled":  disabled,
		"hidden":    hidden,
	}
	err := addSoundCardComponentTmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func uploadedByComponent(username, avatarCDN string) string {
	var builder strings.Builder
	m := map[string]any{
		"username":  username,
		"avatarCDN": avatarCDN,
	}
	err := uploadedByComponentTmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func init() {
	addSoundCardComponentTmpl = template.Must(template.New("addSoundCardComponentTmpl").Parse(addSoundCardComponentTmplRaw))
	soundCardComponentTmpl = template.Must(template.New("soundCardComponentTmpl").Parse(soundCardComponentTmplRaw))
	soundCardComponent2Tmpl = template.Must(template.New("soundCardComponentTmpl").Parse(soundCardComponent2TmplRaw))
	uploadedByComponentTmpl = template.Must(template.New("uploadedByComponentTmpl").Parse(uploadedByComponentTmplRaw))
}
