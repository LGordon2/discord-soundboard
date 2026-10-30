import { isSwitching, isSoundSwitching, switchSound } from './swap-feedback';

const mobile = window.matchMedia?.('(max-width: 639px), (pointer: coarse)');
let selected: {name: string; ext: string; source: HTMLElement} | null = null;
const active = () => document.body.classList.contains('ui-preview') && Boolean(mobile?.matches);

const clearSelection = (restoreFocus = false) => {
    const source = selected?.source;
    selected?.source.classList.remove('sound-selected');
    selected = null;
    document.getElementById('mobile-slot-picker')?.setAttribute('hidden', '');
    document.querySelectorAll('.slot-pick').forEach(button => button.remove());
    document.querySelectorAll('#playable-sounds .slot-unavailable').forEach(card => card.classList.remove('slot-unavailable'));
    if (restoreFocus && source?.isConnected) source.querySelector<HTMLButtonElement>('[data-choose-slot]')?.focus({preventScroll: true});
};

export const decorateMobilePreview = (root: ParentNode) => {
    if (!active()) return;
    const cards = Array.from(root.querySelectorAll<HTMLElement>('.add-sound-component'));
    if (root instanceof HTMLElement && root.matches('.add-sound-component')) cards.unshift(root);
    for (const card of cards) {
        if (card.querySelector('[data-choose-slot]')) continue;
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'choose-slot';
        button.setAttribute('data-choose-slot', '');
        button.textContent = 'Choose slot';
        button.setAttribute('aria-label', `Choose a slot for ${card.getAttribute('data-soundname') || 'sound'}`);
        card.append(button);
    }
};

export const refreshMobilePreview = () => {
    const starting = active() && !document.body.classList.contains('mobile-preview');
    document.body.classList.toggle('mobile-preview', active());
    if (starting) decorateMobilePreview(document.getElementById('storedsounds') || document);
    if (!active()) { clearSelection(); return; }
    if (!selected) return;
    const uploaded = Array.from(document.querySelectorAll<HTMLElement>('#playable-sounds [data-soundid]'));
    if (isSoundSwitching(selected.name) || uploaded.some(card => card.getAttribute('data-soundname') === selected?.name)) {
        clearSelection(); return;
    }
    let available = 0;
    for (const card of uploaded) {
        const allowed = card.classList.contains('droppable') && !isSwitching(card.id);
        card.classList.toggle('slot-unavailable', !allowed);
        let button = card.querySelector<HTMLButtonElement>('.slot-pick');
        if (!allowed) { button?.remove(); continue; }
        available++;
        if (!button) {
            button = document.createElement('button');
            button.type = 'button';
            button.className = 'slot-pick';
            card.append(button);
        }
        const name = card.getAttribute('data-soundname') || '';
        button.textContent = name ? `Replace ${name}` : 'Use free slot';
        button.setAttribute('aria-label', `${button.textContent} with ${selected.name}`);
        button.setAttribute('data-current-sound', card.getAttribute('data-soundid') || '');
    }
    const label = document.getElementById('slot-picker-label');
    if (label) label.textContent = available ? `Choose a slot for ${selected.name}` : 'No editable slots are available right now. Cancel or wait for a switch to finish.';
};

export const initMobilePreview = () => {
    decorateMobilePreview(document);
    document.getElementById('ui-preview-toggle')?.addEventListener('change', refreshMobilePreview);
    mobile?.addEventListener('change', refreshMobilePreview);
    document.addEventListener('soundboard:switch-state', refreshMobilePreview);
    document.addEventListener('keydown', event => { if (event.key === 'Escape') clearSelection(true); });
    document.addEventListener('click', event => {
        if (!(event.target instanceof Element)) return;
        if (event.target.closest('#cancel-slot-picker')) { clearSelection(true); return; }
        if (!active() || event.target.closest('[inert]')) return;
        const choose = event.target.closest<HTMLButtonElement>('[data-choose-slot]');
        if (choose) {
            const source = choose.closest<HTMLElement>('.add-sound-component');
            const name = source?.getAttribute('data-soundname');
            const ext = source?.getAttribute('data-soundext');
            if (!source || !name || !ext || source.hidden || source.classList.contains('hidden') || isSoundSwitching(name)) return;
            clearSelection();
            selected = {name, ext, source};
            source.classList.add('sound-selected');
            const picker = document.getElementById('mobile-slot-picker');
            const label = document.getElementById('slot-picker-label');
            if (label) label.textContent = `Choose a slot for ${name}`;
            if (picker) { picker.hidden = false; picker.focus({preventScroll: true}); }
            refreshMobilePreview();
            document.getElementById('playable-sounds')?.scrollIntoView?.({block: 'start', behavior: 'instant'});
            return;
        }
        const target = event.target.closest<HTMLButtonElement>('.slot-pick');
        const card = target?.closest<HTMLElement>('#playable-sounds [data-soundid]');
        if (!selected || !target || !card || !card.isConnected || !card.classList.contains('droppable') || isSwitching(card.id) ||
            target.getAttribute('data-current-sound') !== card.getAttribute('data-soundid')) return;
        const {name, ext} = selected;
        clearSelection();
        void switchSound(card, name, ext);
    });
    refreshMobilePreview();
};
