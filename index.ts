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

const makeDroppable = (el: any) => {
    const target = el;
    target.addEventListener('dragover', (event: any) => {
        event.preventDefault();
    })

    target.addEventListener('drop', (event: any) => {
        event.preventDefault();
        let target = event.target;
        event.stopImmediatePropagation();
        while (!target.classList.contains('droppable')) {
            target = target.parentNode;
        }
        if (dragged !== null && target.classList.contains('droppable')) {

            const soundLocation = dragged.getAttribute('data-soundname');
            const soundExtension = dragged.getAttribute('data-soundext');
            if (soundLocation && soundExtension) {
                const soundID = target.getAttribute('data-soundid');
                const body: any = {
                    add: {
                        soundLocation: (soundLocation + soundExtension)
                    }
                };
                if (soundID != null && soundID !== '') {
                    body.delete = {
                        soundID,
                    };
                }

                fetch('/swap-sound', {
                    method: 'POST',
                    headers: {
                        'content-type': 'application/json'
                    },
                    body: JSON.stringify(body)
                });
            }
        }
    })
}

const makeDraggable = (el: any) => {
    el.addEventListener('dragstart', dragStart);
    el.addEventListener('dragend', dragEnd);
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
        disableFn(el.querySelector('button'));
    });
}

(window as any)._makeDraggable = makeDraggable;
(window as any)._makeDroppable = makeDroppable;
// TODO this is pretty much the same as play sound
(window as any)._highlightSound = (elId: any, soundId: string) => {
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