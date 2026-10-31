import { isSwitching, isSoundSwitching, switchSound } from './swap-feedback';
import { decorateLibraryPreview } from './library-preview';
import { createSoundSearch } from './sound-search';
import { decorateSoundMetadata, getSoundMetadata } from './sound-metadata';

const keys = { enabled: 'soundboard-ui-preview', favorites: 'soundboard-favorites', recent: 'soundboard-recent', search: 'soundboard-search', filter: 'soundboard-filter' };

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

let keyboardCards: HTMLElement[] = [];
let keyboardChoice = '';
const filenameOf = (card: HTMLElement) => (card.getAttribute('data-soundname') || '') + (card.getAttribute('data-soundext') || '');
const keyboardResults = () => keyboardCards.filter(card => card.isConnected && !card.hidden && !card.classList.contains('hidden'))
    .sort((a, b) => filter === 'recent' ? Number(a.style.order) - Number(b.style.order) : 0);
const syncKeyboardChoice = () => {
    const cards = keyboardResults();
    const selected = cards.find(card => filenameOf(card) === keyboardChoice) || cards[0];
    keyboardChoice = selected ? filenameOf(selected) : '';
    for (const card of keyboardCards) card.classList.toggle('keyboard-selected', enabled && card === selected);
    const status = document.getElementById('keyboard-selection');
    if (status) {
        const ready = document.activeElement?.id === 'playable-sounds';
        status.classList.toggle('board-send-ready', ready);
        const message = enabled && ready ? 'Ready to send · Q W E R / A S D F' : enabled && selected ? `Selected: ${selected.getAttribute('data-soundname')}. Shift + Q W E R / A S D F replaces that slot.` : '';
        if (status.textContent !== message) status.textContent = message;
    }
    return selected;
};

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
    syncKeyboardChoice();
};

export const syncLibraryPreview = () => {
    const matchesSearch = createSoundSearch(query);
    let visible = 0;
    let available = 0;
    keyboardCards = [];
    document.querySelectorAll<HTMLElement>('#storedsounds .add-sound-component').forEach(card => {
        keyboardCards.push(card);
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
    syncKeyboardChoice();
};

export const recordRecentSound = (name: string) => {
    if (!enabled || !name) return;
    recent = [name, ...recent.filter(item => item !== name)].slice(0, 24);
    write(keys.recent, JSON.stringify(recent));
    if (enabled && filter === 'recent') syncLibraryPreview();
};

export const initUIPreview = () => {
    const columns = document.getElementById('library-columns') as HTMLSelectElement | null;
    if (columns) {
        const normalize = (value: string) => {
            const number = Number(value);
            return Number.isInteger(number) && number >= 1 && number <= 12 ? number : 4;
        };
        let preferred = normalize(read('soundboard-library-columns') || '4');
        const applyColumns = () => {
            const rem = parseFloat(window.getComputedStyle?.(document.documentElement).fontSize || '16');
            // Cards need 15rem, plus 1rem gaps and 1rem on each side.
            const limit = Math.max(1, Math.min(12, Math.floor((window.innerWidth / rem - 1) / 16)));
            columns.replaceChildren();
            for (let count = 1; count <= limit; count++) {
                const option = document.createElement('option');
                option.value = String(count);
                option.textContent = String(count);
                columns.append(option);
            }
            const maximum = String(Math.min(preferred, limit));
            columns.value = maximum;
            document.documentElement.style.setProperty('--library-columns', maximum);
        };
        applyColumns();
        columns.addEventListener('change', () => {
            preferred = normalize(columns.value);
            write('soundboard-library-columns', String(preferred));
            applyColumns();
        });
        window.addEventListener('resize', applyColumns);
    }
    const toggle = document.getElementById('ui-preview-toggle') as HTMLInputElement | null;
    const tools = document.getElementById('library-tools');
    if (!toggle || !tools) return;
    const search = document.getElementById('library-search') as HTMLInputElement | null;
    if (search) search.value = (read(keys.search) || '').slice(0, 120);
    query = (search?.value || '').trim().toLocaleLowerCase();
    const savedFilter = read(keys.filter);
    filter = savedFilter && ['all', 'favorites', 'recent'].includes(savedFilter) ? savedFilter : 'all';
    document.querySelectorAll('[data-library-filter]').forEach(button => {
        button.setAttribute('aria-pressed', String(button.getAttribute('data-library-filter') === filter));
    });
    document.addEventListener('keydown', event => {
        if (event.isComposing || event.ctrlKey || event.metaKey || event.altKey) return;
        if (!enabled || !search || event.defaultPrevented || event.repeat) return;
        const target = event.target instanceof Element ? event.target : null;
        if (document.querySelector('dialog[open]') || target?.closest('[inert]') || search.closest('[inert]')) return;
        if (event.key === 'Escape' && target === search && !event.shiftKey) {
            event.preventDefault();
            const board = document.getElementById('playable-sounds');
            search.blur();
            if (board) {
                board.setAttribute('tabindex', '-1');
                board.focus({preventScroll: true});
                syncKeyboardChoice();
            }
            return;
        }
        if (event.shiftKey) {
            const slotIndex = 'qwerasdf'.indexOf(event.key.toLowerCase());
            if (event.key.length !== 1 || slotIndex < 0 || (target !== search &&
                target?.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"])'))) return;
            event.preventDefault();
            const source = syncKeyboardChoice();
            const slot = document.getElementById(`soundboard-${slotIndex}`);
            const name = source?.getAttribute('data-soundname');
            const extension = source?.getAttribute('data-soundext');
            if (slot && source && name && extension && slot.classList.contains('droppable') &&
                !slot.hidden && !slot.classList.contains('slot-unavailable') && !slot.closest('[inert]') &&
                !isSwitching(slot.id) && !isSoundSwitching(name) && !source.closest('[inert]')) {
                void switchSound(slot, name, extension);
            }
            return;
        }
        if (target === search && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
            const cards = keyboardResults();
            if (!cards.length) return;
            event.preventDefault();
            const index = cards.findIndex(card => filenameOf(card) === keyboardChoice);
            const next = Math.max(0, Math.min(cards.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1)));
            keyboardChoice = filenameOf(cards[next]!);
            syncKeyboardChoice()?.scrollIntoView?.({block: 'nearest'});
            return;
        }
        if (target?.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"])')) return;
        if (event.key === '/') {
            event.preventDefault();
            search.focus();
            search.select();
        } else if ('qwerasdf'.includes(event.key.toLowerCase()) && event.key.length === 1) {
            const slot = document.getElementById(`soundboard-${'qwerasdf'.indexOf(event.key.toLowerCase())}`);
            const send = slot?.querySelector<HTMLButtonElement>('[data-action="send"]');
            if (slot && !slot.hidden && !slot.classList.contains('slot-unavailable') &&
                !slot.closest('[inert]') && send && !send.disabled) {
                event.preventDefault();
                send.click();
            }
        } else if (event.key === '[' || event.key === ']') {
            const filters = ['all', 'favorites', 'recent'];
            const next = (filters.indexOf(filter) + (event.key === ']' ? 1 : 2)) % filters.length;
            const button = document.querySelector<HTMLButtonElement>(`[data-library-filter="${filters[next]}"]`);
            if (button) { event.preventDefault(); button.click(); }
        }
    });
    document.addEventListener('focusin', syncKeyboardChoice);
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
        const value = (event.target as HTMLInputElement).value.slice(0, 120);
        keyboardChoice = '';
        query = value.trim().toLocaleLowerCase();
        write(keys.search, value);
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
            write(keys.filter, filter);
            document.querySelectorAll('[data-library-filter]').forEach(button => {
                button.setAttribute('aria-pressed', String(button === choice));
            });
            syncLibraryPreview();
        }
    });
    apply();
};
