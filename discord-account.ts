export type DiscordSession = {configured:boolean;canUpload:boolean;isAdmin?:boolean;csrf?:string;user?:{id:string;username:string;avatar?:string}};
const listeners = new Set<(session: DiscordSession) => void>();
let current: DiscordSession | undefined;
let revision = 0;
export const subscribeDiscordSession = (listener: (session: DiscordSession) => void) => {listeners.add(listener);};
const publish = (session: DiscordSession) => {current = session; for (const listener of listeners) listener(session);};
export const loadDiscordSession = async () => {
    const version = ++revision;
    const response = await fetch('/auth/session', {cache:'no-store'});
    if (!response.ok) throw new Error('Could not check Discord login. Try reopening the uploader.');
    const session = await response.json() as DiscordSession;
    if (version === revision) publish(session);
    return current || session;
};
export const signOutDiscord = async (csrf: string) => {
    const response = await fetch('/auth/logout', {method:'POST',headers:{'X-CSRF-Token':csrf}});
    if (!response.ok) throw new Error('Could not sign out. Try again.');
    revision++;
    publish({configured:current?.configured ?? true,canUpload:false});
};
export const discordAvatarURL = (user: NonNullable<DiscordSession['user']>) =>
    /^\d+$/.test(user.id) && user.avatar && /^(?:a_)?[a-f0-9]+$/.test(user.avatar)
        ? `https://cdn.discordapp.com/avatars/${user.id}/${user.avatar}.webp?size=32` : '';

export const initAccountHeader = () => {
    const header = document.getElementById('preview-account')!;
    const login = document.getElementById('preview-account-login')!;
    const identity = document.getElementById('preview-account-identity')!;
    const logout = document.getElementById('preview-account-logout') as HTMLButtonElement;
    const error = document.getElementById('preview-account-error')!;
    const enabled = () => document.body.classList.contains('ui-preview');
    const menu = document.getElementById('preview-account-menu')!;
    const menuToggle = document.getElementById('preview-account-menu-toggle') as HTMLButtonElement;
    const navigation = document.getElementById('preview-account-navigation')!;
    const analyticsLink = document.getElementById('account-analytics-link')!;
    const adminLink = document.getElementById('account-admin-link')!;
    const closeMenu = () => {navigation.hidden=true;menuToggle.setAttribute('aria-expanded','false');};
    const openMenu = () => {navigation.hidden=false;menuToggle.setAttribute('aria-expanded','true');};
    menuToggle.addEventListener('click', () => {if(navigation.hidden)openMenu();else closeMenu();});
    menuToggle.addEventListener('keydown', event => {
        if(event.key==='ArrowDown') {event.preventDefault();openMenu();navigation.querySelector<HTMLElement>('input, a:not([hidden])')?.focus();}
    });
    document.addEventListener('click', event => {if(event.target instanceof Element && !menu.contains(event.target))closeMenu();});
    document.addEventListener('keydown', event => {
        if(event.key==='Escape' && !navigation.hidden) {event.preventDefault();closeMenu();menuToggle.focus();}
    });
    navigation.addEventListener('click', event => {if(event.target instanceof Element && event.target.closest('a'))closeMenu();});
    const shortcuts = document.getElementById('keyboard-shortcuts') as HTMLDialogElement;
    document.getElementById('open-keyboard-shortcuts')?.addEventListener('click', () => {
        closeMenu();
        shortcuts.showModal();
    });
    document.getElementById('close-keyboard-shortcuts')?.addEventListener('click', () => shortcuts.close());
    shortcuts.addEventListener('close', () => menuToggle.focus());
    const soundSettings = document.querySelector<HTMLElement>('.sound-settings')!;
    const syncSettings = () => {
        const destination = document.getElementById(enabled() ? 'preview-sound-settings' : 'sound-settings-home')!;
        if(soundSettings.parentElement !== destination)destination.append(soundSettings);
    };

    const render = (session: DiscordSession) => {
        header.hidden = !enabled();
        identity.hidden = !session.user;
        logout.hidden = !session.user;
        login.hidden = !session.configured || Boolean(session.user);
        menu.hidden = !enabled();
        analyticsLink.hidden = !session.user || !session.canUpload;
        adminLink.hidden = !session.user || !session.isAdmin;
        closeMenu();
        identity.replaceChildren();
        if (!session.user) return;
        const label = document.createElement('span');
        label.textContent = `Signed in as ${session.user.username}`;
        const url = discordAvatarURL(session.user);
        if (url) {const avatar = document.createElement('img');avatar.src=url;avatar.alt='';avatar.width=32;avatar.height=32;identity.append(avatar);}
        identity.append(label);
    };
    subscribeDiscordSession(render);
    const refresh = async () => {
        syncSettings();
        render(current || {configured:false,canUpload:false});
        if (!enabled()) {header.hidden=true;closeMenu();return;}
        try {await loadDiscordSession();} catch {render(current || {configured:false,canUpload:false});}
    };
    document.getElementById('ui-preview-toggle')!.addEventListener('change', () => {void refresh();});
    window.addEventListener('focus', () => {void refresh();});
    logout.addEventListener('click', async () => {
        if (!current?.csrf || logout.disabled) return;
        logout.disabled=true;error.textContent='';
        try {await signOutDiscord(current.csrf);} catch (failure) {error.textContent=failure instanceof Error?failure.message:'Could not sign out. Try again.';}
        finally {logout.disabled=false;}
    });
    void refresh();
};
