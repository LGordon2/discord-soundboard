import { recordRecentSound } from './ui-preview';

type PendingSwap = {
    name: string;
    oldSoundID: string;
    accepted: boolean;
    timer?: ReturnType<typeof setTimeout>;
};

const pendingSwaps = new Map<string, PendingSwap>();
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
};

// Discord sends intermediate empty cards over the WebSocket. Reapply the
// overlay to replacement DOM nodes until both HTTP and the new card confirm.
export const reconcileSwitches = () => {
    for (const [id, pending] of pendingSwaps) {
        const card = document.getElementById(id);
        if (!card) continue;
        const soundID = card.getAttribute('data-soundid');
        if (pending.accepted && soundID && soundID !== pending.oldSoundID &&
            card.getAttribute('data-soundname') === pending.name) {
            finish(id);
            recordRecentSound(pending.name);
            announce(`${pending.name} is ready.`, false, 3500);
            continue;
        }
        if (card.querySelector('.switch-overlay')) continue;
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
        label.textContent = `Switching to ${pending.name}…`;
        const spinner = document.createElement('span');
        spinner.className = 'switch-spinner';
        spinner.setAttribute('aria-hidden', 'true');
        overlay.append(spinner, label);
        card.append(overlay);
    }
};

export const isSwitching = (id: string) => pendingSwaps.has(id);

export const switchSound = async (card: HTMLElement, name: string, extension: string) => {
    const id = card.id;
    if (isSwitching(id)) return;
    const oldSoundID = card.getAttribute('data-soundid') || '';
    const pending: PendingSwap = { name, oldSoundID, accepted: false };
    pendingSwaps.set(id, pending);
    announce(`Switching to ${name}…`);
    reconcileSwitches();
    try {
        const response = await fetch('/swap-sound', {
            method: 'POST',
            headers: { 'content-type': 'application/json' },
            body: JSON.stringify({
                add: { soundLocation: name + extension },
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
