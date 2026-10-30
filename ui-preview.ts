import { decorateLibraryPreview } from './library-preview';
import { createSoundSearch } from './sound-search';
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
    if (button.getAttribute('aria-pressed') === String(selected)) return;
    button.textContent = selected ? '★' : '☆';
    button.setAttribute('aria-pressed', String(selected));
    button.setAttribute('aria-label', `${selected ? 'Remove from' : 'Add to'} favorites: ${filename}`);
    button.title = selected ? 'Remove from favorites' : 'Add to favorites';
};

let availableCount = 0;
let visibleCount = 0;
const countedCards = new WeakMap<HTMLElement, {available: boolean; visible: boolean}>();
const showLibraryCounts = () => {
    const count = document.getElementById('library-count');
    const loading = document.getElementById('storedsounds')?.getAttribute('aria-busy') === 'true';
    const text = loading ? 'Loading sounds…' : `${visibleCount} of ${availableCount} available sounds`;
    if (count && count.textContent !== text) count.textContent = text;
    const empty = document.getElementById('library-empty');
    if (empty) empty.hidden = !enabled || loading || visibleCount > 0;
};

// Availability changes leave search, favorites, metadata and ordering intact.
// Adjust the cached counts using only cards whose uploaded status changed.
export const syncLibraryAvailability = (cards: HTMLElement[]) => {
    for (const card of cards) {
        const previous = countedCards.get(card);
        if (!previous) { syncLibraryPreview(); return; }
        const available = !card.classList.contains('hidden');
        const visible = available && !card.hidden;
        availableCount += Number(available) - Number(previous.available);
        visibleCount += Number(visible) - Number(previous.visible);
        countedCards.set(card, {available, visible});
    }
    showLibraryCounts();
};

export const syncLibraryPreview = () => {
    const matchesSearch = createSoundSearch(query);
    let visible = 0;
    let available = 0;
    document.querySelectorAll<HTMLElement>('#storedsounds .add-sound-component').forEach(card => {
        decorateLibraryPreview(card);
        const name = card.getAttribute('data-soundname') || '';
        const filename = name + (card.getAttribute('data-soundext') || '');
        let favorite = card.querySelector<HTMLButtonElement>('[data-favorite]');
        if (enabled && !favorite) {
            favorite = document.createElement('button');
            favorite.type = 'button';
            favorite.className = 'favorite-button';
            favorite.setAttribute('data-favorite', filename);
            card.querySelector('.sound-header')?.prepend(favorite);
        }
        if (enabled && favorite) updateFavorite(favorite, filename);
        if (!enabled) favorite?.remove();
        const recentIndex = recent.indexOf(name);
        const matches = matchesSearch(name, getSoundMetadata(name)?.description || '') &&
            (filter !== 'favorites' || favorites.has(filename)) &&
            (filter !== 'recent' || recentIndex >= 0);
        card.hidden = enabled && !matches;
        if (enabled && filter === 'recent') card.style.order = String(recentIndex);
        else card.style.removeProperty('order');
        const isAvailable = !card.classList.contains('hidden');
        const isVisible = isAvailable && !card.hidden;
        countedCards.set(card, {available: isAvailable, visible: isVisible});
        if (isAvailable) available++;
        if (isVisible) visible++;
    });
    decorateSoundMetadata(document);
    availableCount = available;
    visibleCount = visible;
    showLibraryCounts();
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
        document.documentElement.classList.toggle('ui-preview', enabled);
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
