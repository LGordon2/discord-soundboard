import { renderEmojiSelection } from './emoji-selection';
import { isSingleEmojiValue, singleEmojiMessage } from './single-emoji';
type SoundMetadata = { description: string; emoji_id?: string; emoji_name?: string };
let metadata: Record<string, SoundMetadata> = Object.create(null);
let onUpdate = () => {};
let requestVersion = 0;
let metadataReady = false;
const decoratedCards = new WeakMap<HTMLElement, { enabled: boolean; name: string; entry: SoundMetadata | undefined }>();

export const getSoundMetadata = (name: string): SoundMetadata | undefined => metadata[name];

export const customEmojiChoices = () => {
    const ids = new Set<string>();
    return Object.entries(metadata).flatMap(([sound, value]) => {
        if (!value.emoji_id || !/^\d+$/.test(value.emoji_id) || ids.has(value.emoji_id)) return [];
        ids.add(value.emoji_id);
        return [{value: value.emoji_id, label: `${sound} custom emoji`, group: 'Custom', image: `https://cdn.discordapp.com/emojis/${value.emoji_id}.webp?size=32`}];
    });
};

const applyMetadata = (value: unknown) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return;
    const next: Record<string, SoundMetadata> = Object.create(null);
    for (const [name, entry] of Object.entries(value)) {
        if (!entry || typeof entry !== 'object' || typeof entry.description !== 'string') continue;
        next[name] = {
            description: entry.description,
            ...(typeof entry.emoji_id === 'string' ? { emoji_id: entry.emoji_id } : {}),
            ...(typeof entry.emoji_name === 'string' ? { emoji_name: entry.emoji_name } : {}),
        };
    }
    metadata = next;
    metadataReady = true;
    decorateSoundMetadata(document);
    onUpdate();
};

export const refreshSoundMetadata = async () => {
    const version = ++requestVersion;
    try {
        const response = await fetch('/sound-metadata', { signal: AbortSignal.timeout(10000) });
        if (response.ok) {
            const values: unknown = await response.json();
            if (version === requestVersion) applyMetadata(values);
        }
    } catch { /* Sound controls remain usable when details are unavailable. */ }
    finally {
        if (version === requestVersion) {
            metadataReady = true;
            releaseInitialLayout();
        }
    }
};

const releaseInitialLayout = () => {
    if (metadataReady && document.getElementById('playable-sounds')?.getAttribute('aria-busy') !== 'true') {
        document.getElementById('initial-card-layout')?.remove();
    }
};

export const decorateSoundMetadata = (root: ParentNode) => {
    const cards = Array.from(root.querySelectorAll<HTMLElement>('[data-soundname]'));
    if (root instanceof HTMLElement && root.matches('[data-soundname]')) cards.unshift(root);
    const enabled = document.body.classList.contains('ui-preview');
    for (const card of cards) {
        const name = card.getAttribute('data-soundname') || '';
        if (!name) continue;
        const entry = metadata[name];
        const descriptionText = entry?.description.trim() || '';
        card.classList.toggle('has-description', enabled && descriptionText.length > 0);
        const previous = decoratedCards.get(card);
        if (previous?.enabled === enabled && previous.name === name && previous.entry === entry) continue;
        decoratedCards.set(card, { enabled, name, entry });
        // Rebuild only metadata decorations; leave controls and their state intact.
        card.querySelectorAll('.sound-emoji, .sound-description, [data-edit-description]').forEach(node => node.remove());
        if (!enabled) { card.removeAttribute('title'); continue; }
        const header = card.querySelector<HTMLElement>('.sound-header');
        if (!header) continue;
        const edit = document.createElement('button');
        edit.type = 'button';
        edit.className = 'sound-details-button';
        edit.setAttribute('data-edit-description', name);
        edit.setAttribute('aria-label', `Edit sound details: ${name}`);
        edit.title = 'Sound description and emoji';
        edit.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-9h.01"/></svg>';
        header.append(edit);
        card.removeAttribute('title');
        if (descriptionText) {
            const description = document.createElement('p');
            description.className = 'sound-description';
            description.textContent = descriptionText;
            if (card.classList.contains('add-sound-component')) card.append(description);
            else header.after(description);
        }
        const heading = card.querySelector('h5');
        if (heading && (entry?.emoji_id || entry?.emoji_name)) {
            const emoji = document.createElement('span');
            emoji.className = 'sound-emoji';
            if (entry.emoji_id && /^\d+$/.test(entry.emoji_id)) {
                const image = document.createElement('img');
                image.src = `https://cdn.discordapp.com/emojis/${entry.emoji_id}.webp?size=32`;
                image.alt = 'Custom emoji';
                image.loading = 'lazy';
                emoji.append(image);
            } else { emoji.textContent = entry.emoji_name || ''; }
            heading.prepend(emoji);
        }
    }
    releaseInitialLayout();
};

export const initSoundMetadata = (updated: () => void) => {
    onUpdate = updated;
    const dialog = document.getElementById('sound-details') as HTMLDialogElement | null;
    const form = document.getElementById('sound-details-form') as HTMLFormElement | null;
    const input = document.getElementById('sound-description') as HTMLTextAreaElement | null;
    const emojiInput = document.getElementById('sound-emoji') as HTMLInputElement | null;
    const pickerButton = document.getElementById('choose-sound-emoji') as HTMLButtonElement | null;
    const pickerRoot = document.getElementById('sound-emoji-picker');
    const clearEmoji = document.getElementById('clear-sound-emoji');
    const syncClearEmoji = () => {
        if (clearEmoji) clearEmoji.hidden = !emojiInput?.value.trim();
        if (pickerButton) renderEmojiSelection(pickerButton, emojiInput?.value || '');
    };
    emojiInput?.addEventListener('input', syncClearEmoji);
    emojiInput?.addEventListener('change', syncClearEmoji);
    let pickerVersion = 0;
    const hidePicker = () => {
        pickerVersion++;
        if (pickerRoot) {pickerRoot.hidden = true; pickerRoot.replaceChildren();}
        pickerButton?.setAttribute('aria-expanded', 'false');
    };
    pickerButton?.addEventListener('click', async () => {
        if (!pickerRoot || !emojiInput || saving) return;
        if (!pickerRoot.hidden) {hidePicker(); return;}
        const version = ++pickerVersion;
        pickerRoot.hidden = false;
        pickerRoot.textContent = 'Loading emoji…';
        pickerButton.setAttribute('aria-expanded', 'true');
        try {
            const { mountEmojiPicker } = await import('./emoji-picker');
            if (version !== pickerVersion || !dialog?.hasAttribute('open')) return;
            mountEmojiPicker(pickerRoot, customEmojiChoices(), value => {
                emojiInput.value = value;
                syncClearEmoji();
                hidePicker();
                pickerButton.focus();
            });
        } catch {
            if (version === pickerVersion) pickerRoot.textContent = 'Could not load the emoji picker. Close it and try again.';
        }
    });
    document.getElementById('clear-sound-emoji')?.addEventListener('click', () => {
        if (emojiInput && !saving) {emojiInput.value = ''; syncClearEmoji(); hidePicker(); pickerButton?.focus();}
    });
    dialog?.addEventListener('close', hidePicker);
    dialog?.addEventListener('keydown', event => {
        if (event.key === 'Escape' && pickerRoot && !pickerRoot.hidden) {
            event.preventDefault(); event.stopPropagation(); hidePicker(); pickerButton?.focus();
        }
    });
    const error = document.getElementById('sound-details-error');
    const title = document.getElementById('sound-details-name');
    const save = document.getElementById('save-sound-details') as HTMLButtonElement | null;
    if (!dialog || !form || !input || !emojiInput || !error || !title || !save) return;
    let name = '';
    let initialEmoji = '';
    let saving = false;
    document.addEventListener('click', event => {
        const button = event.target instanceof Element ? event.target.closest('[data-edit-description]') : null;
        if (!button || saving || button.closest('[inert]')) return;
        hidePicker();
        name = button.getAttribute('data-edit-description') || '';
        title.textContent = name;
        input.value = metadata[name]?.description || '';
        initialEmoji = metadata[name]?.emoji_id || metadata[name]?.emoji_name || '';
        emojiInput.value = initialEmoji;
        syncClearEmoji();
        error.textContent = '';
        dialog.showModal();
        input.focus();
    });
    document.getElementById('close-sound-details')?.addEventListener('click', () => { if (!saving) dialog.close(); });
    dialog.addEventListener('cancel', event => { if (saving) event.preventDefault(); });
    form.addEventListener('submit', async event => {
        event.preventDefault();
        if (saving) return;
        if (!isSingleEmojiValue(emojiInput.value)) {error.textContent = singleEmojiMessage;pickerButton?.focus();return;}
        saving = true;
        hidePicker();
        save.disabled = true;
        save.textContent = 'Saving…';
        error.textContent = '';
        ++requestVersion; // Invalidate a metadata fetch started before this edit.
        try {
            const response = await fetch('/sound-metadata', {
                method: 'PUT', headers: { 'content-type': 'application/json' },
                body: JSON.stringify({ soundName: name, description: input.value, ...(emojiInput.value !== initialEmoji ? {emoji: emojiInput.value} : {}) }),
                signal: AbortSignal.timeout(10000),
            });
            if (!response.ok) throw new Error((await response.text()).trim() || 'Could not save sound details.');
            applyMetadata(await response.json());
            dialog.close();
        } catch (failure) { error.textContent = failure instanceof Error ? failure.message : 'Could not save sound details. Please try again.'; }
        finally { saving = false; save.disabled = false; save.textContent = 'Save details'; }
    });
    void refreshSoundMetadata();
};
