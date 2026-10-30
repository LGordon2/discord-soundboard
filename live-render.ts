// Markup is local and fixed. All server-provided text is escaped, and CDN path
// components are encoded; socket data never supplies HTML or executable URLs.
const escape = (value: unknown) => String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!));
const control = (action: string, icon: string, disabled = false, extra = '') =>
    `<button class="sound-control ${extra}" data-action="${action}" data-icon="${icon}"${disabled ? ' disabled' : ''}></button>`;

type Slot = {slot: number; id?: string; name?: string; canSend?: boolean; canSave?: boolean; canRemove?: boolean; username?: string; avatar?: string; guild?: string};
const card = (slot: Slot) => {
    if (!Number.isInteger(slot.slot) || slot.slot < 0 || slot.slot > 7) throw Error('Invalid slot');
    const used = Boolean(slot.id && slot.name);
    const outer = `<div id="soundboard-${slot.slot}" class="sound-card${slot.canRemove || !used ? ' droppable' : ''}" data-soundid="${escape(slot.id)}" data-soundname="${escape(slot.name)}">`;
    if (!used) return `${outer}<div class="sound-empty"><h5>Free</h5></div></div>`;
    // The avatar URL is built from server-owned Discord IDs. Restrict it to the
    // CDN anyway, so future protocol changes cannot introduce arbitrary URLs.
    const avatar = slot.avatar?.startsWith('https://cdn.discordapp.com/avatars/') ? slot.avatar : '';
    return `${outer}<div class="sound-header"><h5 class="sound-title">${escape(slot.name)}</h5>${slot.canSave ? control('save','save',false,'sound-save') : ''}<a class="sound-control sound-download" href="https://cdn.discordapp.com/soundboard-sounds/${encodeURIComponent(slot.id!)}" data-icon="download" aria-label="Download ${escape(slot.name)}"></a></div><div class="sound-actions">${control('preview','headphones')}${control('send','play',!slot.canSend)}<button class="sound-control sound-delete" data-action="delete" data-icon="minus" data-soundid="${escape(slot.id)}" data-guildid="${escape(slot.guild)}"${slot.canRemove ? '' : ' disabled'}></button>${slot.canRemove ? '' : `<div class="uploader-tooltip"><span>Uploaded by</span><img src="${escape(avatar)}?size=32" alt=""><span class="uploader-name">${escape(slot.username)}</span></div>`}</div></div>`;
};

type LibraryFile = {name: string; ext: string} | [string, string];
const libraryCard = (file: LibraryFile) => {
    const [name, ext] = Array.isArray(file) ? file : [file.name, file.ext];
    if (typeof name !== 'string' || (ext !== '.ogg' && ext !== '.mp3')) throw Error('Invalid library entry');
    return `<div draggable="true" class="add-sound-component" data-soundname="${escape(name)}" data-soundext="${escape(ext)}"><div class="sound-header"><h5 class="sound-title library-title">${escape(name)}</h5>${control('add','plus')}</div></div>`;
};

export const renderLiveEvents = (wire: string): string => {
    // Legacy fragments remain readable during rolling/cached-client upgrades.
    if (wire.trimStart().startsWith('<')) return wire;
    return wire.split('\n').filter(line => line.trim()).map(line => {
        const event = JSON.parse(line);
        switch (event.type) {
            case 'board': {
                const slots = (event.slots || []).map(card).join('');
                let html = event.full ? `<div id="playable-sounds" role="region" aria-label="Uploaded soundboard">${slots}</div>` : slots;
                html += `<div id="addsoundscript" data-hidden-sounds="${escape(JSON.stringify(event.hidden || []))}" data-has-empty="${Boolean(event.hasEmpty)}"></div>`;
                if (event.library) html += `<div id="storedsounds" role="region" aria-label="Sound library">${event.library.map(libraryCard).join('')}</div>`;
                return html;
            }
            case 'switches': return `<div id="switchstate" data-value="${escape(JSON.stringify(event.switches))}"></div>`;
            case 'metadata': return '<div id="soundmetadata" data-refresh="true"></div>';
            case 'played': return `<div id="playsound" data-play-sound="${escape(event.soundID)}"></div>`;
            case 'viewers': return `<span id="user-count">${Number.isSafeInteger(event.count) ? event.count : 0}</span>`;
            default: return '';
        }
    }).join('');
};
