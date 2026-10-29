export type UpdateHooks = {
    hydrate: (root: Element) => void;
    library: (hidden: string[], hasEmpty: boolean) => void;
    played: (soundID: string) => void;
    updated: () => void;
};

// The server sends fragments keyed by these IDs. Apply the whole message before
// updating library controls, so a library replacement cannot undo their state.
export const applyBoardUpdate = (html: string, hooks: UpdateHooks) => {
    const template = document.createElement('template');
    template.innerHTML = html;
    const roots: Element[] = [];
    let libraryChanged = false;
    let played: string | null = null;
    for (const node of Array.from(template.content.children)) {
        const id = node.id;
        if (!/^(soundboard-\d+|playable-sounds|storedsounds|addsoundscript|playsound|user-count)$/.test(id)) continue;
        // Updates are declarative; never execute scripts received over a socket.
        node.querySelectorAll('script').forEach(script => script.remove());
        if (id === 'playsound') {
            played = node.getAttribute('data-play-sound');
            continue;
        }
        const current = document.getElementById(id);
        if (!current) continue;
        current.replaceWith(node);
        roots.push(node);
        libraryChanged ||= id === 'storedsounds' || id === 'addsoundscript';
    }
    roots.forEach(hooks.hydrate);
    if (libraryChanged) {
        const config = document.getElementById('addsoundscript');
        const hidden: unknown = JSON.parse(config?.getAttribute('data-hidden-sounds') || '[]');
        if (Array.isArray(hidden) && hidden.every(name => typeof name === 'string')) {
            hooks.library(hidden, config?.getAttribute('data-has-empty') === 'true');
        }
    }
    if (played) hooks.played(played);
    hooks.updated();
};

export const connectBoard = (hooks: UpdateHooks) => {
    let socket: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    let stopped = false;
    const status = document.createElement('div');
    status.id = 'connection-status';
    status.setAttribute('role', 'status');
    document.body.append(status);

    const connect = () => {
        if (stopped) return;
        status.textContent = 'Connecting to soundboard…';
        const url = new URL('/ws', window.location.href);
        url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
        const connection = new WebSocket(url);
        socket = connection;
        connection.onopen = () => {
            attempts = 0;
            status.textContent = '';
        };
        connection.onmessage = event => {
            try {
                applyBoardUpdate(event.data as string, hooks);
            } catch (error) {
                console.error('Could not apply soundboard update', error);
                status.textContent = 'Could not update the board. Refresh to reconnect.';
            }
        };
        connection.onclose = () => {
            if (socket !== connection) return;
            socket = null;
            if (stopped) return;
            status.textContent = 'Connection lost. Reconnecting…';
            const delay = Math.min(30000, 500 * 2 ** Math.min(attempts++, 6));
            retry = setTimeout(connect, delay + Math.random() * 250);
        };
        connection.onerror = () => connection.close();
    };
    window.addEventListener('pagehide', () => {
        stopped = true;
        clearTimeout(retry);
        socket?.close();
    });
    window.addEventListener('pageshow', event => {
        if (event.persisted) {
            stopped = false;
            connect();
        }
    });
    connect();
};
