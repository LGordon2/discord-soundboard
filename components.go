package main

import (
	"html/template"
	"strings"
)

var (
	addSoundCardComponentTmpl *template.Template
	soundCardComponentTmpl    *template.Template
	uploadedByComponentTmpl   *template.Template
)

const uploadedByComponentTmplRaw = `
    <div class="uploader-tooltip">
        <span>Uploaded by</span>
        <img src="{{.avatarCDN}}?size=32" alt="">
        <span class="uploader-name">{{.username}}</span>
    </div>
`

const addSoundCardComponentTmplRaw = `
    <div draggable="true" data-soundname="{{.soundName}}" data-soundext="{{.extension}}"
        class="add-sound-component {{if .hidden}}hidden{{end}}">
        <div class="sound-header">
            <h5 class="sound-title library-title">{{.soundName}}</h5>
            <button class="sound-control" data-action="add" data-icon="plus"></button>
        </div>
    </div>
`

const soundCardComponentTmplRaw = `
<div data-soundname="{{.soundName}}" data-soundid="{{.soundId}}" id="soundboard-{{.ordinal}}"
     class="sound-card {{if .canRemove}}droppable{{end}}">
    {{if .used}}
    <div class="sound-header">
        <h5 class="sound-title">{{.soundName}}</h5>
        {{if .canSave}}<button data-action="save" data-icon="save" class="sound-control sound-save"></button>{{end}}
        <a class="sound-control sound-download" href="https://cdn.discordapp.com/soundboard-sounds/{{.soundId}}"
           data-icon="download" aria-label="Download {{.soundName}}"></a>
    </div>
    <div class="sound-actions">
        <button data-action="preview" data-icon="headphones" class="sound-control"></button>
        <button data-action="send" data-icon="play" class="sound-control" {{if not .canSend}}disabled="true"{{end}}></button>
        {{if .deleteButton}}{{.deleteButton}}{{end}}
    </div>
    {{else}}
    <div class="sound-empty"><h5>Free</h5></div>
    {{end}}
</div>
`

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
	if button, ok := deleteButton.(string); ok {
		m["deleteButton"] = template.HTML(button) // Generated internally by deleteButton.
	}
	err := soundCardComponentTmpl.Execute(&builder, m)
	if err != nil {
		panic(err)
	}
	return builder.String()
}

func addSoundCardComponent(storedSound, extension, guildID string, hidden bool) string {
	var builder strings.Builder
	m := map[string]any{
		"soundName":        storedSound,
		"soundNameEscaped": storedSound,
		"extension":        extension,
		"guildID":          guildID,
		"hidden":           hidden,
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
	uploadedByComponentTmpl = template.Must(template.New("uploadedByComponentTmpl").Parse(uploadedByComponentTmplRaw))
}
