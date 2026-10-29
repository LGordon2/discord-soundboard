type PendingSwap = {
    name: string;
    oldSoundID: string;
    accepted: boolean;
    timer?: ReturnType<typeof setTimeout>;
};

const pendingSwaps = new Map<string, PendingSwap>();

const announce = (message: string, error = false) => {
    let status = document.getElementById('swap-status');
    if (!status) {
        status = document.createElement('div');
        status.id = 'swap-status';
        status.setAttribute('role', 'status');
        status.setAttribute('aria-live', 'polite');
        document.body.append(status);
    }
    status.classList.toggle('swap-error', error);
    status.textContent = message;
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
const reconcile = () => {
    for (const [id, pending] of pendingSwaps) {
        const card = document.getElementById(id);
        if (!card) continue;
        const soundID = card.getAttribute('data-soundid');
        if (pending.accepted && soundID && soundID !== pending.oldSoundID &&
            card.querySelector('h5')?.textContent?.trim() === pending.name) {
            finish(id);
            announce(`${pending.name} is ready.`);
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

new MutationObserver(reconcile).observe(document.body, { childList: true, subtree: true });

export const isSwitching = (id: string) => pendingSwaps.has(id);

export const switchSound = async (card: HTMLElement, name: string, extension: string) => {
    const id = card.id;
    if (isSwitching(id)) return;
    const oldSoundID = card.getAttribute('data-soundid') || '';
    const pending: PendingSwap = { name, oldSoundID, accepted: false };
    pendingSwaps.set(id, pending);
    announce(`Switching to ${name}…`);
    reconcile();
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
        reconcile();
    } catch {
        finish(id);
        announce(`Could not confirm the switch to ${name}. Refresh to check the board before trying again.`, true);
    }
};
