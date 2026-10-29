// import * as amplitude from '@amplitude/analytics-browser';
// import { sessionReplayPlugin } from '@amplitude/plugin-session-replay-browser';
import { enableDragDropTouch } from '@dragdroptouch/drag-drop-touch';
import { isSwitching, switchSound, reconcileSwitches } from './swap-feedback';
import { connectBoard } from './live-updates';
import { initUIPreview, syncLibraryPreview, recordRecentSound } from './ui-preview';
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
            iconSvg = `<svg class="h-8 w-8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">  <polygon points="5 3 19 12 5 21 5 3"></polygon></svg>`
            break;
        case 'minus':
            iconSvg = `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
	<path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14"></path>
  </svg>`
            break;
        case 'download':
            iconSvg = `<svg
                        class="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                        stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                        <polyline points="7 10 12 15 17 10" />
                        <line x1="12" y1="15" x2="12" y2="3" />
                    </svg>`
            break;
        case 'save':
            iconSvg = `<svg class="h-6 w-6"  viewBox="0 0 24 24"  fill="none"  stroke="currentColor"  stroke-width="2"  stroke-linecap="round"  stroke-linejoin="round">  <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />  <polyline points="17 21 17 13 7 13 7 21" />  <polyline points="7 3 7 8 15 8" /></svg>`
            break;
        case 'headphones':
            iconSvg = `<svg class="h-8 w-8 text-yellow-500"  width="24" height="24" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none" stroke-linecap="round" stroke-linejoin="round">  <path stroke="none" d="M0 0h24v24H0z"/>  <rect x="4" y="13" rx="2" width="5" height="7" />  <rect x="15" y="13" rx="2" width="5" height="7" />  <path d="M4 15v-3a8 8 0 0 1 16 0v3" /></svg>`
            break;
        case 'plus':
            iconSvg = `<svg class="h-8 w-8"
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
    const classesToAdd = ['border-green-400']
    const classesToRemove = ['border-gray-200', 'dark:border-gray-700']

    el.classList.add(...classesToAdd)
    el.classList.remove(...classesToRemove)
    const timeoutId = setTimeout(() => {
        el.classList.add(...classesToRemove)
        el.classList.remove(...classesToAdd)
    }, audio.duration * 1000)
    playTimeouts[soundId] = timeoutId;
}

const playingAudio: Record<string, any> = {};

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
    if (playingAudio[soundId]) {
        playingAudio[soundId].pause();
    }
    const audio = new Audio(`https://cdn.discordapp.com/soundboard-sounds/${soundId}`);
    playingAudio[soundId] = audio;
    // Count actual user previews once, excluding muted/failed playback and the
    // automatic local echoes of sends to Discord.
    if (!sent) {
        audio.addEventListener('playing', () => {
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
        const classesToAdd = ['border-sky-400']
        const classesToRemove = ['border-gray-200', 'dark:border-gray-700']

        el.classList.add(...classesToAdd)
        el.classList.remove(...classesToRemove)
    })
}

const dragEnd = () => {
    document.querySelectorAll('.droppable').forEach((el) => {
        const classesToRemove = ['border-sky-400']
        const classesToAdd = ['border-gray-200', 'dark:border-gray-700']

        el.classList.add(...classesToAdd)
        el.classList.remove(...classesToRemove)
    })
}

const addSoundUpdates = (hiddenSounds: any, hasEmpty: boolean) => {
    let disableFn = (el: any) => el.setAttribute('disabled', 'true');
    if (hasEmpty) {
        disableFn = (el) => el.removeAttribute('disabled');
    }
    document.querySelectorAll(".add-sound-component").forEach((el) => {
        const soundName = el.getAttribute('data-soundname');
        if (soundName && hiddenSounds.includes(soundName)) { // should be hidden
            el.classList.add('hidden');
        } else {
            el.classList.remove('hidden');
        }
        disableFn(el.querySelector('button[data-action="add"]'));
    });
    syncLibraryPreview();
}

// TODO this is pretty much the same as play sound
(window as any)._highlightSound = (elId: any, soundId: string, _soundName: string) => {
    // amplitude.track('soundboard: play sound', { soundName })
    const audio = new Audio(`https://cdn.discordapp.com/soundboard-sounds/${soundId}`);
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
        case 'add': {
            endpoint = '/add-sound';
            const stored = button.closest('.add-sound-component');
            query.set('soundLocation', (stored?.getAttribute('data-soundname') || '') + (stored?.getAttribute('data-soundext') || ''));
            break;
        }
        default: return;
    }
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    try {
        const response = await fetch(`${endpoint}?${query}`, { method });
        if (!response.ok) throw new Error('Sound action failed');
        if (action === 'send') recordRecentSound(card?.getAttribute('data-soundname') || '');
        if (action === 'add') recordRecentSound(button.closest('.add-sound-component')?.getAttribute('data-soundname') || '');
        document.getElementById('action-status')?.replaceChildren();
    } catch {
        showActionError();
    } finally {
        button.removeAttribute('aria-busy');
        // Replaced buttons retain server permissions; only restore this
        // request's original button and account for current library capacity.
        button.disabled = false;
        if (action === 'add') {
            const config = document.getElementById('addsoundscript');
            if (config?.getAttribute('data-has-empty') !== 'true') button.disabled = true;
        }
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
initSoundMetadata(syncLibraryPreview);
enableDragDropTouch();
connectBoard({
    hydrate,
    library: addSoundUpdates,
    played: soundID => playSound(null, soundID, true),
    updated: reconcileSwitches,
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