import { decorateSoundMetadata, getSoundMetadata } from './sound-metadata';

const keys = { enabled: 'soundboard-ui-preview', favorites: 'soundboard-favorites', recent: 'soundboard-recent' };

const read = (key: string): string | null => {
    try { return localStorage.getItem(key); } catch { return null; }
};
const write = (key: string, value: string) => {
    try { localStorage.setItem(key, value); } catch { /* Keep this session usable when storage is unavailable. */ }
};
const readList = (key: string): string[] => {
    try {
        const value: unknown = JSON.parse(read(key) || '[]');
        return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string').slice(0, 500) : [];
    } catch { return []; }
};

const favorites = new Set(readList(keys.favorites));
let recent = readList(keys.recent).slice(0, 24);
let filter = 'all';
let query = '';
let enabled = false;

const updateFavorite = (button: HTMLButtonElement, filename: string) => {
    const selected = favorites.has(filename);
    button.textContent = selected ? '★' : '☆';
    button.setAttribute('aria-pressed', String(selected));
    button.setAttribute('aria-label', `${selected ? 'Remove from' : 'Add to'} favorites: ${filename}`);
    button.title = selected ? 'Remove from favorites' : 'Add to favorites';
};

export const syncLibraryPreview = () => {
    let visible = 0;
    let available = 0;
    document.querySelectorAll<HTMLElement>('#storedsounds .add-sound-component').forEach(card => {
        const name = card.getAttribute('data-soundname') || '';
        const filename = name + (card.getAttribute('data-soundext') || '');
        let favorite = card.querySelector<HTMLButtonElement>('[data-favorite]');
        if (enabled && !favorite) {
            favorite = document.createElement('button');
            favorite.type = 'button';
            favorite.className = 'favorite-button';
            favorite.setAttribute('data-favorite', filename);
            card.querySelector('.flex-row')?.prepend(favorite);
        }
        if (enabled && favorite) updateFavorite(favorite, filename);
        if (!enabled) favorite?.remove();
        const recentIndex = recent.indexOf(name);
        const matches = `${name} ${getSoundMetadata(name)?.description || ''}`.toLocaleLowerCase().includes(query) &&
            (filter !== 'favorites' || favorites.has(filename)) &&
            (filter !== 'recent' || recentIndex >= 0);
        card.hidden = enabled && !matches;
        if (enabled && filter === 'recent') card.style.order = String(recentIndex);
        else card.style.removeProperty('order');
        if (!card.classList.contains('hidden')) {
            available++;
            if (!card.hidden) visible++;
        }
    });
    decorateSoundMetadata(document);
    const count = document.getElementById('library-count');
    const text = `${visible} of ${available} available sounds`;
    if (count && count.textContent !== text) count.textContent = text;
    const empty = document.getElementById('library-empty');
    if (empty) empty.hidden = !enabled || visible > 0;
};

export const recordRecentSound = (name: string) => {
    if (!enabled || !name) return;
    recent = [name, ...recent.filter(item => item !== name)].slice(0, 24);
    write(keys.recent, JSON.stringify(recent));
    if (enabled && filter === 'recent') syncLibraryPreview();
};

export const initUIPreview = () => {
    const toggle = document.getElementById('ui-preview-toggle') as HTMLInputElement | null;
    const tools = document.getElementById('library-tools');
    if (!toggle || !tools) return;
    enabled = read(keys.enabled) === 'true';
    toggle.checked = enabled;
    const apply = () => {
        document.body.classList.toggle('ui-preview', enabled);
        tools.hidden = !enabled;
        syncLibraryPreview();
    };
    toggle.addEventListener('change', () => {
        enabled = toggle.checked;
        write(keys.enabled, String(enabled));
        apply();
    });
    document.getElementById('library-search')?.addEventListener('input', event => {
        query = (event.target as HTMLInputElement).value.trim().toLocaleLowerCase();
        syncLibraryPreview();
    });
    document.addEventListener('click', event => {
        if (!enabled || !(event.target instanceof Element)) return;
        const favorite = event.target.closest<HTMLButtonElement>('[data-favorite]');
        if (favorite) {
            const filename = favorite.getAttribute('data-favorite') || '';
            if (favorites.has(filename)) favorites.delete(filename);
            else if (favorites.size < 500) favorites.add(filename);
            write(keys.favorites, JSON.stringify([...favorites]));
            syncLibraryPreview();
        }
        const choice = event.target.closest<HTMLButtonElement>('[data-library-filter]');
        if (choice) {
            filter = choice.getAttribute('data-library-filter') || 'all';
            document.querySelectorAll('[data-library-filter]').forEach(button => {
                button.setAttribute('aria-pressed', String(button === choice));
            });
            syncLibraryPreview();
        }
    });
    apply();
};
