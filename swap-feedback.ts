import { recordRecentSound } from './ui-preview';

type PendingSwap = {
    name: string;
    oldSoundID: string;
    knownSoundIDs: Set<string>;
    accepted: boolean;
    timer?: ReturnType<typeof setTimeout>;
};

const pendingSwaps = new Map<string, PendingSwap>();
const sharedSwaps = new Map<string, string>();
let sharedEpoch = '';
let sharedVersion = -1;
let announcementTimer: ReturnType<typeof setTimeout> | undefined;

const announce = (message: string, error = false, dismissAfter = 0) => {
    clearTimeout(announcementTimer);
    let status = document.getElementById('swap-status');
    if (!status) {
        status = document.createElement('div');
        status.id = 'swap-status';
        status.setAttribute('role', 'status');
        status.setAttribute('aria-live', 'polite');
        document.body.append(status);
    }
    const dismiss = () => {
        clearTimeout(announcementTimer);
        status.replaceChildren();
    };
    status.classList.toggle('swap-error', error);
    const text = document.createElement('span');
    text.textContent = message;
    const close = document.createElement('button');
    close.type = 'button';
    close.className = 'toast-close';
    close.setAttribute('aria-label', 'Dismiss notification');
    close.textContent = '×';
    close.addEventListener('click', dismiss);
    status.replaceChildren(text, close);
    if (dismissAfter > 0) {
        const progress = document.createElement('div');
        progress.className = 'toast-countdown';
        progress.setAttribute('aria-hidden', 'true');
        progress.style.animationDuration = `${dismissAfter}ms`;
        status.title = `Dismisses automatically in ${dismissAfter / 1000} seconds`;
        status.append(progress);
        announcementTimer = setTimeout(dismiss, dismissAfter);
    } else {
        status.removeAttribute('title');
    }
};

const clearCard = (id: string) => {
    const card = document.getElementById(id);
    if (!card) return;
    card.removeAttribute('aria-busy');
    card.classList.remove('sound-switching');
    card.querySelector('.switch-overlay')?.remove();
    card.querySelectorAll<HTMLElement>('[data-switch-inert]').forEach(child => {
        child.inert = false;
        child.removeAttribute('data-switch-inert');
    });
};

const finish = (id: string) => {
    const pending = pendingSwaps.get(id);
    if (pending?.timer) clearTimeout(pending.timer);
    pendingSwaps.delete(id);
    clearCard(id);
    const shared = sharedSwaps.get(id);
    if (shared) drawSwitch(id, shared);
    document.dispatchEvent(new Event('soundboard:switch-state'));
};

// Discord sends intermediate empty cards over the WebSocket. Reapply the
// overlay to replacement DOM nodes until both HTTP and the new card confirm.
export const reconcileSwitches = () => {
    for (const [id, pending] of pendingSwaps) {
        const card = document.getElementById(id);
        if (!card) continue;
        const available = Array.from(document.querySelectorAll<HTMLElement>('#playable-sounds [data-soundid]')).some(sound => {
            const id = sound.getAttribute('data-soundid');
            return id && !pending.knownSoundIDs.has(id) && sound.getAttribute('data-soundname') === pending.name;
        });
        if (pending.accepted && available) {
            finish(id);
            recordRecentSound(pending.name);
            announce(`${pending.name} is ready.`, false, 3500);
            continue;
        }
        drawSwitch(id, pending.name);
    }
    for (const [id, name] of sharedSwaps) if (!pendingSwaps.has(id)) drawSwitch(id, name);
};

const drawSwitch = (id: string, name: string) => {
    const card = document.getElementById(id);
    if (!card) return;
    const existing = card.querySelector('.switch-overlay');
    if (existing) {
        const label = existing.querySelector('.switch-label');
        if (label) label.textContent = `Switching to ${name}…`;
        return;
    }
    card.classList.add('sound-switching');
    card.setAttribute('aria-busy', 'true');
    for (const child of Array.from(card.children)) {
        if (child instanceof HTMLElement && !child.inert) {
            child.inert = true;
            child.setAttribute('data-switch-inert', '');
        }
    }
    const overlay = document.createElement('div');
    overlay.className = 'switch-overlay';
    const label = document.createElement('span');
    label.className = 'switch-label';
    label.textContent = `Switching to ${name}…`;
    const spinner = document.createElement('span');
    spinner.className = 'switch-spinner';
    spinner.setAttribute('aria-hidden', 'true');
    overlay.append(spinner, label);
    card.append(overlay);
};

export const applySharedSwitches = (value: unknown) => {
    if (!value || typeof value !== 'object') return;
    const snapshot = value as { epoch?: unknown; version?: unknown; pending?: unknown };
    if (typeof snapshot.epoch !== 'string' || typeof snapshot.version !== 'number' ||
        !Number.isSafeInteger(snapshot.version) || snapshot.version < 0 || !Array.isArray(snapshot.pending)) return;
    if (snapshot.epoch === sharedEpoch && snapshot.version <= sharedVersion) return;
    const next = new Map<string, string>();
    for (const item of snapshot.pending) {
        if (!item || typeof item !== 'object' || !Number.isInteger(item.slot) || item.slot < 0 || item.slot >= 8 ||
            typeof item.name !== 'string' || item.name.length === 0) return;
        next.set(`soundboard-${item.slot}`, item.name);
    }
    sharedEpoch = snapshot.epoch;
    sharedVersion = snapshot.version;
    const previous = [...sharedSwaps.keys()];
    sharedSwaps.clear();
    for (const [id, name] of next) sharedSwaps.set(id, name);
    for (const id of previous) if (!sharedSwaps.has(id) && !pendingSwaps.has(id)) clearCard(id);
    reconcileSwitches();
    document.dispatchEvent(new Event('soundboard:switch-state'));
};

export const isSwitching = (id: string) => pendingSwaps.has(id) || sharedSwaps.has(id);
export const switchingSoundNames = () => new Set([
    ...Array.from(pendingSwaps.values(), pending => pending.name), ...sharedSwaps.values(),
]);
export const isSoundSwitching = (name: string) => {
    for (const pending of pendingSwaps.values()) if (pending.name === name) return true;
    for (const shared of sharedSwaps.values()) if (shared === name) return true;
    return false;
};

export const switchSound = async (card: HTMLElement, name: string, extension: string) => {
    const id = card.id;
    if (isSwitching(id) || isSoundSwitching(name)) return;
    const oldSoundID = card.getAttribute('data-soundid') || '';
    const knownSoundIDs = new Set(Array.from(document.querySelectorAll('#playable-sounds [data-soundid]'), sound => sound.getAttribute('data-soundid') || ''));
    const pending: PendingSwap = { name, oldSoundID, knownSoundIDs, accepted: false };
    pendingSwaps.set(id, pending);
    document.dispatchEvent(new Event('soundboard:switch-state'));
    announce(`Switching to ${name}…`);
    reconcileSwitches();
    try {
        const response = await fetch('/swap-sound', {
            method: 'POST',
            headers: { 'content-type': 'application/json' },
            body: JSON.stringify({
                add: { soundLocation: name + extension },
                slot: Number(id.replace('soundboard-', '')),
                ...(oldSoundID ? { delete: { soundID: oldSoundID } } : {}),
            }),
        });
        if (!response.ok) throw new Error('Swap rejected');
        pending.accepted = true;
        // A disconnected WebSocket must not leave the controls locked forever.
        pending.timer = setTimeout(() => {
            finish(id);
            announce(`${name} was uploaded, but the board has not updated. Refresh to sync with Discord.`, true);
        }, 20000);
        reconcileSwitches();
    } catch {
        finish(id);
        announce(`Could not confirm the switch to ${name}. Refresh to check the board before trying again.`, true);
    }
};
