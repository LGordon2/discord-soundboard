package main

import (
	"bytes"
	"text/template"
)

const soundCardComponentTmpl = `
<div id="box-{{.soundId}}"
            class="h-24 min-w-72 max-w-sm p-2 m-2 bg-white border border-2 border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700 grid grid-cols-1 divide-y divide-gray-700">
            {{ if .used }}
            <div class="flex flex-row">
                <h5 class="flex-1 mb-2 text-xl font-bold tracking-tight text-gray-900 dark:text-white">{{.soundName}}
                </h5>
                <a class="shrink text-sky-500"
                    href="https://cdn.discordapp.com/soundboard-sounds/{{.soundId}}"><svg
                        class="h-6 w-6 text-blue-500" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                        stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                        <polyline points="7 10 12 15 17 10" />
                        <line x1="12" y1="15" x2="12" y2="3" />
                    </svg></a>
            </div>

            <div class="flex flex-row divide-x divide-gray-700">
                <button onclick="window._playSound('{{.soundId}}')" class="flex flex-1 items-center justify-center mt-1"><svg class="h-8 w-8 text-gray-500"
                        viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
                        stroke-linejoin="round">
                        <polygon points="5 3 19 12 5 21 5 3" />
                    </svg></button>
                {{.deleteButton}}
                <!--<button class="flex flex-1 items-center justify-center mt-1" {{if .disabled}}disabled="true"{{end}}></button>-->
            </div>
            {{ else }}
            <div class="flex items-center justify-center text-center">
                <h5 class="text-xl text-slate-400">Free</h5>
            </div>
            {{ end }}
        </div>
`

func soundCardComponent(id, name string, deleteButton any) string {
	var buf bytes.Buffer
	m := map[string]any{
		"soundId":      id,
		"soundName":    name,
		"deleteButton": deleteButton,
		"used":         id != "" && name != "" && deleteButton != nil,
	}
	err := template.Must(template.New("soundCardComponentTmpl").Parse(soundCardComponentTmpl)).Execute(&buf, m)
	if err != nil {
		panic(err)
	}
	return buf.String()
}
