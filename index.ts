import { initLibraryPreview, stopLibraryPreview } from './library-preview';
import { initMobilePreview, decorateMobilePreview, refreshMobilePreview } from './mobile-preview';
// import * as amplitude from '@amplitude/analytics-browser';
// import { sessionReplayPlugin } from '@amplitude/plugin-session-replay-browser';
import { enableDragDropTouch } from '@dragdroptouch/drag-drop-touch';
import { isSwitching, switchSound, reconcileSwitches, applySharedSwitches, switchingSoundNames } from './swap-feedback';
import { connectBoard } from './live-updates';
import { initUIPreview, syncLibraryPreview, syncLibraryAvailability, recordRecentSound } from './ui-preview';
import { initSoundMetadata, decorateSoundMetadata, refreshSoundMetadata } from './sound-metadata';

const playTimeouts: Record<string, any> = {};
(window as any)._addStats = {};

const toggleState = (id: string) => {
    const state = window.localStorage.getItem(id) === 'true';
    const el = document.getElementById(id);
    if (!el || !('checked' in el)) {
        return;
    }
    el.checked = state;
    const _toggleState = {
        update: (newState: any) => {
            (window as any).localStorage.setItem(id, newState);
            el.checked = newState;
            _toggleState.state = newState;
        },
        state,
    }
    return _toggleState;
}

let muted: any = toggleState('mute-sounds-checkbox');
let shouldPlaySendSounds: any = toggleState('play-send-sounds-checkbox');

const configurePlaySendSoundEl = () => {
    const el = document.querySelector('#playsound') ?? document.querySelector('#playsounddisabled')
    if (el) {
        if (shouldPlaySendSounds.state) {
            el.setAttribute('id', 'playsound')
        } else {
            el.setAttribute('id', 'playsounddisabled')
        }
    }
}

configurePlaySendSoundEl()

const iconLoad = (el: any, iconType: any) => {
    let iconSvg;
    switch (iconType) {
        case 'play':
            iconSvg = `<svg class="control-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">  <polygon points="5 3 19 12 5 21 5 3"></polygon></svg>`
            break;
        case 'minus':
            iconSvg = `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="control-icon control-icon-small">
	<path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14"></path>
  </svg>`
            break;
        case 'download':
            iconSvg = `<svg
                        class="control-icon control-icon-small" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                        stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                        <polyline points="7 10 12 15 17 10" />
                        <line x1="12" y1="15" x2="12" y2="3" />
                    </svg>`
            break;
        case 'save':
            iconSvg = `<svg class="control-icon control-icon-small"  viewBox="0 0 24 24"  fill="none"  stroke="currentColor"  stroke-width="2"  stroke-linecap="round"  stroke-linejoin="round">  <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />  <polyline points="17 21 17 13 7 13 7 21" />  <polyline points="7 3 7 8 15 8" /></svg>`
            break;
        case 'headphones':
            iconSvg = `<svg class="control-icon"  width="24" height="24" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none" stroke-linecap="round" stroke-linejoin="round">  <path stroke="none" d="M0 0h24v24H0z"/>  <rect x="4" y="13" rx="2" width="5" height="7" />  <rect x="15" y="13" rx="2" width="5" height="7" />  <path d="M4 15v-3a8 8 0 0 1 16 0v3" /></svg>`
            break;
        case 'plus':
            iconSvg = `<svg class="control-icon"
                        viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
                        stroke-linejoin="round">
                        <line x1="12" y1="5" x2="12" y2="19"></line>
                        <line x1="5" y1="12" x2="19" y2="12"></line>
                    </svg>`;
            break;
        default:
            break;
    }
    el.innerHTML = iconSvg
}

const highlightSound = (el: any, audio: any, soundId: string) => {
    if (playTimeouts[soundId]) {
        clearTimeout(playTimeouts[soundId]);
    }
    el.classList.add('sound-playing');
    const timeoutId = setTimeout(() => {
        el.classList.remove('sound-playing');
    }, audio.duration * 1000)
    playTimeouts[soundId] = timeoutId;
}

const playingAudio: Record<string, any> = {};
let previewVersion = 0;
const stopBoardPreviews = () => {
    previewVersion++;
    Object.values(playingAudio).forEach(audio => audio.pause());
    Object.values(playTimeouts).forEach(clearTimeout);
    document.querySelectorAll('#playable-sounds .sound-playing').forEach(card => card.classList.remove('sound-playing'));
};


/**
 *    Mute sounds | Play send sounds | IsSendSound? || Sound played?
 * 1.     F       |  F               | F            || T
 * 2.     F       |  F               | T            || F
 * 3.     F       |  T               | F            || F
 * 4.     F       |  T               | T            || T
 * 5.     T       |  x               | x            || F
 * 
 * The second parameter indicates that the sound was sent through the websocket.
 */
const playSound = (elId: any, soundId: string, sent: boolean) => {
    sent = sent === undefined ? false : sent
    if (sent && (muted.state || !shouldPlaySendSounds.state)) return;
    if (!sent) {
        stopLibraryPreview();
        stopBoardPreviews();
    }
    const version = previewVersion;
    if (playingAudio[soundId]) {
        playingAudio[soundId].pause();
    }
    const audio = new Audio(`https://cdn.discordapp.com/soundboard-sounds/${soundId}`);
    playingAudio[soundId] = audio;
    // Count actual user previews once, excluding muted/failed playback and the
    // automatic local echoes of sends to Discord.
    if (!sent) {
        audio.addEventListener('playing', () => {
            if (version !== previewVersion) return;
            recordRecentSound(document.getElementById(elId)?.getAttribute('data-soundname') || '');
            void fetch('/analytics/play', {
                method: 'POST',
                headers: { 'content-type': 'application/json' },
                body: JSON.stringify({ soundID: soundId }),
                keepalive: true,
            }).catch(() => { /* Analytics must never interrupt playback. */ });
        }, { once: true });
    }
    let el = undefined;
    if (elId) {
        el = document.querySelector('#' + elId)
    }
    audio.addEventListener("canplaythrough", () => {
        if (!sent && version !== previewVersion) return;
        if (el) {
            highlightSound(el, audio, soundId)
        }
        if (!muted.state) {
            if (!sent || sent && shouldPlaySendSounds.state) {
                audio.play();
            }
        }
    });
}

const muteSounds = (ev: any) => {
    muted.update(ev.target.checked)
    if (ev.target.checked) {stopLibraryPreview(); stopBoardPreviews();}
}

const playSendSounds = (ev: any) => {
    shouldPlaySendSounds.update(ev.target.checked)
    configurePlaySendSoundEl()
}



let dragged: HTMLElement | null = null;

const dragStart = (e: any) => {
    dragged = e.target;
    document.querySelectorAll('.droppable').forEach((el) => {
        if (isSwitching(el.id)) return;
        el.classList.add('sound-drop-target');
    })
}

const dragEnd = () => {
    document.querySelectorAll('.droppable').forEach((el) => {
        el.classList.remove('sound-drop-target');
    })
}

const freeSoundSlot = () => Array.from(document.querySelectorAll<HTMLElement>('#playable-sounds .droppable[data-soundid=""]'))
    .find(card => !isSwitching(card.id));

let libraryRoot: HTMLElement | null = null;
const libraryCards = new Map<string, HTMLElement[]>();
let hiddenLibraryNames = new Set<string>();
let previousPending = new Set<string>();
let previousAvailable: boolean | undefined;
const indexLibrary = () => {
    const root = document.getElementById('storedsounds');
    if (root === libraryRoot) return false;
    libraryRoot = root;
    libraryCards.clear();
    root?.querySelectorAll<HTMLElement>('.add-sound-component').forEach(card => {
        const name = card.getAttribute('data-soundname') || '';
        const cards = libraryCards.get(name) || [];
        cards.push(card);
        libraryCards.set(name, cards);
    });
    hiddenLibraryNames = new Set();
    previousPending = new Set();
    previousAvailable = undefined;
    return true;
};

const updateAddButtons = (hasEmpty: boolean) => {
    indexLibrary();
    const available = hasEmpty && Boolean(freeSoundSlot());
    const pendingNames = switchingSoundNames();
    const names = previousAvailable !== available ? libraryCards.keys() :
        new Set([...previousPending, ...pendingNames].filter(name => previousPending.has(name) !== pendingNames.has(name)));
    for (const name of names) for (const card of libraryCards.get(name) || []) {
        const button = card.querySelector<HTMLButtonElement>('button[data-action="add"]');
        if (!button) continue;
        const pending = pendingNames.has(name);
        button.disabled = !available || pending;
        if (pending && button.getAttribute('aria-busy') !== 'true') {
            button.setAttribute('aria-busy', 'true');
            button.innerHTML = '<span class="switch-spinner" aria-hidden="true"></span>';
        } else if (!pending && button.getAttribute('aria-busy') === 'true') {
            button.removeAttribute('aria-busy');
            iconLoad(button, 'plus');
        }
        button.setAttribute('aria-label', `${pending ? 'Adding' : 'Add'} ${name}`);
    }
    previousPending = pendingNames;
    previousAvailable = available;
};

document.addEventListener('soundboard:switch-state', () => {
    updateAddButtons(document.getElementById('addsoundscript')?.getAttribute('data-has-empty') === 'true');
});

const addSoundUpdates = (hiddenSounds: string[], hasEmpty: boolean) => {
    const replaced = indexLibrary();
    const next = new Set(hiddenSounds);
    const names = replaced ? libraryCards.keys() :
        new Set([...hiddenLibraryNames, ...next].filter(name => hiddenLibraryNames.has(name) !== next.has(name)));
    const changed: HTMLElement[] = [];
    for (const name of names) for (const card of libraryCards.get(name) || []) {
        card.classList.toggle('hidden', next.has(name));
        changed.push(card);
    }
    hiddenLibraryNames = next;
    updateAddButtons(hasEmpty);
    if (replaced) syncLibraryPreview();
    else syncLibraryAvailability(changed);
};

// TODO this is pretty much the same as play sound
(window as any)._highlightSound = (elId: any, soundId: string, _soundName: string) => {
    // amplitude.track('soundboard: play sound', { soundName })
    const audio = new Audio(`https://cdn.discordapp.com/soundboard-sounds/${soundId}`);
    audio.preload = 'metadata';
    let el = undefined;
    if (elId) {
        el = document.querySelector('#' + elId);
    }
    audio.addEventListener("canplaythrough", () => {
        if (el) {
            highlightSound(el, audio, soundId);
        }
    });
};
(window as any)._playSound = playSound;
(window as any)._muteSounds = muteSounds;
(window as any)._playSendSounds = playSendSounds;
(window as any)._iconLoad = iconLoad;
(window as any)._addSoundUpdates = addSoundUpdates;

const hydrate = (root: Element) => {
    root.querySelectorAll<HTMLElement>('[data-icon]').forEach(el => {
        const icon = el.getAttribute('data-icon');
        iconLoad(el, icon);
        if (!el.hasAttribute('aria-label')) {
            el.setAttribute('aria-label', ({ plus: 'Add sound', minus: 'Remove sound', save: 'Save sound',
                play: 'Play in Discord', headphones: 'Preview sound' } as Record<string, string>)[icon || ''] || 'Sound control');
        }
    });
    decorateSoundMetadata(root);
    decorateMobilePreview(root);
};

const showActionError = () => {
    let status = document.getElementById('action-status');
    if (!status) {
        status = document.createElement('div');
        status.id = 'action-status';
        status.setAttribute('role', 'alert');
        document.body.append(status);
    }
    status.textContent = 'The sound action failed. Check the board before trying again.';
};

document.addEventListener('click', async event => {
    const button = event.target instanceof Element ? event.target.closest<HTMLButtonElement>('button[data-action]') : null;
    if (!button || button.disabled || button.closest('[inert]')) return;
    const card = button.closest<HTMLElement>('[data-soundid]');
    if (card && isSwitching(card.id)) return;
    const soundID = card?.getAttribute('data-soundid') || '';
    const action = button.getAttribute('data-action');
    if (action === 'preview') {
        playSound(card?.id, soundID, false);
        return;
    }
    if (action === 'add') {
        const stored = button.closest('.add-sound-component');
        const name = stored?.getAttribute('data-soundname');
        const extension = stored?.getAttribute('data-soundext');
        const slot = freeSoundSlot();
        if (slot && name && extension) await switchSound(slot, name, extension);
        return;
    }
    const query = new URLSearchParams();
    let endpoint: string;
    let method = 'POST';
    switch (action) {
        case 'send':
            endpoint = '/send-sound';
            query.set('soundID', soundID);
            (window as any)._highlightSound(card?.id, soundID, card?.getAttribute('data-soundname'));
            break;
        case 'save':
            endpoint = '/save-sound';
            query.set('soundID', soundID);
            query.set('soundName', card?.getAttribute('data-soundname') || '');
            break;
        case 'delete':
            endpoint = '/delete-sound';
            method = 'DELETE';
            query.set('soundID', button.getAttribute('data-soundid') || soundID);
            query.set('guildID', button.getAttribute('data-guildid') || '');
            break;
        default: return;
    }
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    try {
        const response = await fetch(`${endpoint}?${query}`, { method });
        if (!response.ok) throw new Error('Sound action failed');
        if (action === 'send') recordRecentSound(card?.getAttribute('data-soundname') || '');
        document.getElementById('action-status')?.replaceChildren();
    } catch {
        showActionError();
    } finally {
        button.removeAttribute('aria-busy');
        // Replacement nodes already carry the server's current permissions.
        button.disabled = false;
    }
});

document.addEventListener('dragstart', event => {
    const card = event.target instanceof Element ? event.target.closest<HTMLElement>('.add-sound-component') : null;
    if (card) dragStart({ target: card });
});
document.addEventListener('dragend', () => { dragEnd(); dragged = null; });
document.addEventListener('dragover', event => {
    if (event.target instanceof Element && event.target.closest('.droppable')) event.preventDefault();
});
document.addEventListener('drop', event => {
    const card = event.target instanceof Element ? event.target.closest<HTMLElement>('.droppable') : null;
    if (!card) return;
    event.preventDefault();
    if (!dragged || isSwitching(card.id)) return;
    const name = dragged.getAttribute('data-soundname');
    const extension = dragged.getAttribute('data-soundext');
    if (name && extension) void switchSound(card, name, extension);
});
document.getElementById('mute-sounds-checkbox')?.addEventListener('change', muteSounds);
document.getElementById('play-send-sounds-checkbox')?.addEventListener('change', playSendSounds);

initUIPreview();
initLibraryPreview(recordRecentSound, () => Boolean(muted.state), stopBoardPreviews);
initMobilePreview();
initSoundMetadata(syncLibraryPreview);
let uploadReady: Promise<() => void> | undefined;
document.getElementById('open-sound-upload')?.addEventListener('click', async () => {
    if (!document.body.classList.contains('ui-preview')) return;
    try {
        uploadReady ||= import('./sound-upload').then(module => module.initSoundUpload());
        const open = await uploadReady;
        open();
    } catch {
        uploadReady = undefined;
        const status = document.getElementById('upload-status');
        if (status) {status.hidden = false; status.textContent = 'Could not load the uploader. Try again.';}
    }
});
enableDragDropTouch();
connectBoard({
    hydrate,
    library: addSoundUpdates,
    played: soundID => playSound(null, soundID, true),
    updated: () => { reconcileSwitches(); refreshMobilePreview(); },
    switches: applySharedSwitches,
    metadata: () => { void refreshSoundMetadata(); },
});

(async () => {
    // Create and Install Session Replay Plugin
    // const sessionReplayTracking = sessionReplayPlugin({ sampleRate: 1, useWebWorker: true, storeType: 'memory' });
    // amplitude.add(sessionReplayTracking);

    // // Your existing initialization logic with Browser SDK
    // // Using lewg project. Logged in via google
    // amplitude.init('f79ec16f47063ce0fca0b7191abe627');
    // (window as any)._amplitude = amplitude;
})()