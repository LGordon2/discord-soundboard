type SoundMetadata = { description: string; emoji_id?: string; emoji_name?: string };
let metadata: Record<string, SoundMetadata> = Object.create(null);
let onUpdate = () => {};
let requestVersion = 0;
let metadataReady = false;
const decoratedCards = new WeakMap<HTMLElement, { enabled: boolean; name: string; entry: SoundMetadata | undefined }>();

export const getSoundMetadata = (name: string): SoundMetadata | undefined => metadata[name];

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
        edit.setAttribute('aria-label', `Edit description: ${name}`);
        edit.title = 'Sound description and source';
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
    const error = document.getElementById('sound-details-error');
    const title = document.getElementById('sound-details-name');
    const save = document.getElementById('save-sound-details') as HTMLButtonElement | null;
    if (!dialog || !form || !input || !error || !title || !save) return;
    let name = '';
    let saving = false;
    document.addEventListener('click', event => {
        const button = event.target instanceof Element ? event.target.closest('[data-edit-description]') : null;
        if (!button || saving || button.closest('[inert]')) return;
        name = button.getAttribute('data-edit-description') || '';
        title.textContent = name;
        input.value = metadata[name]?.description || '';
        error.textContent = '';
        dialog.showModal();
        input.focus();
    });
    document.getElementById('close-sound-details')?.addEventListener('click', () => { if (!saving) dialog.close(); });
    dialog.addEventListener('cancel', event => { if (saving) event.preventDefault(); });
    form.addEventListener('submit', async event => {
        event.preventDefault();
        if (saving) return;
        saving = true;
        save.disabled = true;
        save.textContent = 'Saving…';
        error.textContent = '';
        ++requestVersion; // Invalidate a metadata fetch started before this edit.
        try {
            const response = await fetch('/sound-metadata', {
                method: 'PUT', headers: { 'content-type': 'application/json' },
                body: JSON.stringify({ soundName: name, description: input.value }),
                signal: AbortSignal.timeout(10000),
            });
            if (!response.ok) throw new Error('Save failed');
            applyMetadata(await response.json());
            dialog.close();
        } catch { error.textContent = 'Could not save the description. Please try again.'; }
        finally { saving = false; save.disabled = false; save.textContent = 'Save description'; }
    });
    void refreshSoundMetadata();
};
