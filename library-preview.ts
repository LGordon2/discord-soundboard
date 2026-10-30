let current: {audio: HTMLAudioElement; button: HTMLButtonElement; card: HTMLElement} | null = null;
let recent = (_name: string) => {};
let muted = () => false;
let stopOtherAudio = () => {};

const resetButton = (button: HTMLButtonElement) => {
    button.removeAttribute('aria-busy');
    button.setAttribute('aria-pressed', 'false');
    button.textContent = 'Preview';
};
export const stopLibraryPreview = () => {
    const previous = current;
    current = null;
    if (!previous) return;
    previous.audio.pause();
    previous.card.classList.remove('sound-playing');
    resetButton(previous.button);
};
export const decorateLibraryPreview = (card: HTMLElement) => {
    if (!document.body.classList.contains('ui-preview') || card.querySelector('[data-library-preview]')) return;
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'library-preview-button';
    button.setAttribute('data-library-preview', '');
    button.setAttribute('aria-label', `Preview ${card.getAttribute('data-soundname') || 'sound'}`);
    resetButton(button);
    card.append(button);
};
const showError = (message: string) => {
    let status = document.getElementById('library-preview-status');
    if (!status) {
        status = document.createElement('p');
        status.id = 'library-preview-status';
        status.setAttribute('role', 'status');
        document.getElementById('library-tools')?.append(status);
    }
    status.textContent = message;
};
export const initLibraryPreview = (onRecent: (name: string) => void, isMuted: () => boolean, stopOthers: () => void) => {
    recent = onRecent; muted = isMuted; stopOtherAudio = stopOthers;
    document.getElementById('ui-preview-toggle')?.addEventListener('change', () => {
        if (!document.body.classList.contains('ui-preview')) stopLibraryPreview();
    });
    window.addEventListener('pagehide', stopLibraryPreview);
    document.addEventListener('click', event => {
        const button = event.target instanceof Element ? event.target.closest<HTMLButtonElement>('[data-library-preview]') : null;
        if (!button || !document.body.classList.contains('ui-preview') || button.closest('[inert]')) return;
        const card = button.closest<HTMLElement>('.add-sound-component');
        if (!card || card.hidden || card.classList.contains('hidden')) return;
        if (current?.button === button) {stopLibraryPreview(); return;}
        stopLibraryPreview();
        if (muted()) {showError('Unmute sounds to preview.'); return;}
        stopOtherAudio();
        const name = card.getAttribute('data-soundname') || '';
        const filename = name + (card.getAttribute('data-soundext') || '');
        const audio = new Audio(`/library-audio?${new URLSearchParams({file: filename})}`);
        current = {audio, button, card};
        button.setAttribute('aria-busy', 'true');
        button.setAttribute('aria-pressed', 'true');
        button.textContent = 'Loading…';
        document.getElementById('library-preview-status')?.replaceChildren();
        audio.addEventListener('playing', () => {
            if (current?.audio !== audio) return;
            button.removeAttribute('aria-busy');
            button.textContent = 'Stop';
            card.classList.add('sound-playing');
            recent(name);
            void fetch('/analytics/play', {method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({soundLocation: filename}), keepalive: true}).catch(() => {});
        }, {once: true});
        audio.addEventListener('ended', () => {if (current?.audio === audio) stopLibraryPreview();});
        const failed = () => {
            if (current?.audio !== audio) return;
            stopLibraryPreview();
            showError(`Could not preview ${name}. Try again or check that your browser supports this audio format.`);
        };
        audio.addEventListener('error', failed);
        // Start within the tap gesture, which also works with mobile autoplay rules.
        void audio.play().catch(failed);
    });
};
