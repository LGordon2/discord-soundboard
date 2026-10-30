import { renderEmojiSelection } from './emoji-selection';
import { isSingleEmojiValue, singleEmojiMessage } from './single-emoji';
import { loadDiscordSession, subscribeDiscordSession } from './discord-account';
import type { DiscordSession } from './discord-account';
import { createUploadEditor } from './upload-editor';
import { customEmojiChoices } from './sound-metadata';

export const initSoundUpload = (stopOtherAudio: () => void = () => {}) => {
    const dialog = document.getElementById('sound-upload') as HTMLDialogElement;
    const form = document.getElementById('sound-upload-form') as HTMLFormElement;
    const fields = document.getElementById('sound-upload-fields') as HTMLFieldSetElement;
    const file = document.getElementById('upload-file') as HTMLInputElement;
    const name = document.getElementById('upload-name') as HTMLInputElement;
    const stepLabel = document.getElementById('upload-step-label')!;
    const accountStatus = document.getElementById('upload-auth-status')!;
    const login = document.getElementById('upload-discord-login')!;
    const account = document.getElementById('upload-account')!;
    const accessMessage = 'Ask the site owner to allow uploads for your Discord account.';
    let csrf = '';
    let canUpload = false;
    const applyAccount = (session: DiscordSession) => {
        csrf = session.csrf || ''; canUpload = session.canUpload && Boolean(csrf);
        account.hidden = Boolean(session.user);
        accountStatus.textContent = session.configured ? 'Sign in to upload sounds.' : 'Discord upload login is not configured yet.';
        login.hidden = !session.configured || Boolean(session.user);
        if (session.user && !canUpload) error.textContent = accessMessage;
        else if (error.textContent === accessMessage) error.textContent = '';
        fields.hidden = !canUpload; fields.disabled = !canUpload; save.disabled = !canUpload; showStep(step);
    };
    const loadAccount = async () => {
        canUpload = false; stepLabel.hidden = true;
        fields.hidden = true; fields.disabled = true; save.disabled = true;
        next.hidden = true; back.hidden = true; save.hidden = true;
        try {applyAccount(await loadDiscordSession());}
        catch (failure) {canUpload = false; account.hidden = false; login.hidden = true; accountStatus.textContent = failure instanceof Error ? failure.message : 'Could not check Discord login.';}
    };
    const audio = document.getElementById('upload-audio-preview') as HTMLAudioElement;
    const emoji = document.getElementById('upload-emoji') as HTMLInputElement;
    const picker = document.getElementById('upload-emoji-picker')!;
    const choose = document.getElementById('upload-choose-emoji')!;
    const clearEmoji = document.getElementById('upload-clear-emoji')!;
    const syncEmojiChoice = () => {renderEmojiSelection(choose, emoji.value);clearEmoji.hidden = !emoji.value;};
    const cancel = document.getElementById('cancel-sound-upload') as HTMLButtonElement;
    const save = document.getElementById('save-sound-upload') as HTMLButtonElement;
    const error = document.getElementById('upload-error')!;
    const next = document.getElementById('upload-next') as HTMLButtonElement;
    const back = document.getElementById('upload-back') as HTMLButtonElement;
    let step = 0;
    const editor = createUploadEditor(audio, valid => {next.disabled = !valid;}, stopOtherAudio);
    const showStep = (value: number) => {
        step = value;
        for (const [index,id] of ['upload-file-step','upload-edit-step','upload-details-step'].entries()) document.getElementById(id)!.hidden = index !== step;
        stepLabel.hidden = !canUpload;
        stepLabel.textContent = ['1. Choose audio','2. Trim and preview','3. Sound details'][step]!;
        back.hidden = !canUpload || step === 0; next.hidden = !canUpload || step === 2; save.hidden = !canUpload || step !== 2;
        next.disabled = !editor.valid();
        if (canUpload && dialog.hasAttribute('open')) document.getElementById('upload-step-label')?.focus({preventScroll:true});
    };
    next.addEventListener('click',()=>{if(editor.valid()){editor.stop();showStep(Math.min(2,step+1));}});
    back.addEventListener('click',()=>{editor.stop();showStep(Math.max(0,step-1));});
    let busy = false;
    let objectURL = '';
    let automaticName = '';
    let pickerVersion = 0;
    const hidePicker = () => {pickerVersion++; picker.hidden = true; picker.replaceChildren(); choose.setAttribute('aria-expanded', 'false');};
    const releaseAudio = () => {
        editor.clear();
        audio.pause?.(); audio.removeAttribute('src'); audio.load?.(); audio.hidden = true;
        if (objectURL) URL.revokeObjectURL(objectURL);
        objectURL = '';
    };
    const open = async () => {
        if (!document.body.classList.contains('ui-preview')) return;
        error.textContent = ''; syncEmojiChoice();
        if (!dialog.hasAttribute('open')) dialog.showModal();
        if (file.files?.[0] && !objectURL) file.dispatchEvent(new Event('change'));
        await loadAccount();
        if (!canUpload) (login.hidden ? cancel : login).focus();
    };
    cancel.addEventListener('click', () => {if (!busy) dialog.close();});
    dialog.addEventListener('cancel', event => {if (busy) event.preventDefault();});
    dialog.addEventListener('close', () => {releaseAudio(); hidePicker();});
    window.addEventListener('pagehide', releaseAudio);
    document.addEventListener('visibilitychange',()=>{if(document.hidden)editor.stop();});
    file.addEventListener('change', () => {
        releaseAudio(); error.textContent = '';
        const selected = file.files?.[0]; if (!selected) {showStep(0);return;}
        if (!/\.(mp3|ogg|oga|wav)$/i.test(selected.name)) {error.textContent = 'Choose an MP3, Ogg, or WAV audio file.'; showStep(0); return;}
        if (selected.size > 10 * 1024 * 1024) {error.textContent = 'Choose a file under 10 MB.'; return;}
        if (!name.value || name.value === automaticName) {
            automaticName = Array.from(selected.name.replace(/\.[^.]+$/, '').replace(/[\\/\x00-\x1f]/g, ' ').trim()).slice(0, 32).join('');
            name.value = automaticName;
        }
        objectURL = URL.createObjectURL(selected); editor.load(selected); audio.src = objectURL; audio.hidden = true; showStep(1);
    });
    const dropzone = document.getElementById('upload-dropzone')!;
    dropzone.addEventListener('dragover', event => {event.preventDefault();});
    dropzone.addEventListener('drop', event => {
        event.preventDefault();
        if (busy || !event.dataTransfer?.files.length) return;
        file.files = event.dataTransfer.files;
        file.dispatchEvent(new Event('change', {bubbles: true}));
    });
    choose.addEventListener('click', async () => {
        if (busy) return;
        if (!picker.hidden) {hidePicker(); return;}
        const version = ++pickerVersion; picker.hidden = false; picker.textContent = 'Loading emoji…'; choose.setAttribute('aria-expanded', 'true');
        try {
            const { mountEmojiPicker } = await import('./emoji-picker');
            if (version !== pickerVersion || !dialog.hasAttribute('open')) return;
            mountEmojiPicker(picker, customEmojiChoices(), value => {emoji.value = value; syncEmojiChoice(); hidePicker(); choose.focus();});
        } catch {if (version === pickerVersion) picker.textContent = 'Could not load emoji. Close the picker and try again.';}
    });
    clearEmoji.addEventListener('click', () => {if (busy) return;emoji.value='';syncEmojiChoice();hidePicker();choose.focus();});
    form.addEventListener('submit', async event => {
        event.preventDefault(); if (busy || !document.body.classList.contains('ui-preview')) return;
        const selected = file.files?.[0];
        if (!canUpload || !csrf) {error.textContent = 'Sign in to upload sounds.'; return;}
        if (!selected) {error.textContent = 'Choose an audio file.'; return;}
        if (selected.size > 10 * 1024 * 1024) {error.textContent = 'Choose a file under 10 MB.'; return;}
        if (!editor.valid()) {error.textContent = 'Choose a valid clip up to five seconds.';showStep(1);return;}
        if (step !== 2) {showStep(2);return;}
        if (!isSingleEmojiValue(emoji.value)) {error.textContent = singleEmojiMessage;choose.focus();return;}
        editor.stop();
        const clip = editor.selection();
        const body = new FormData();
        body.append('file', selected); body.append('name', name.value.trim());
        body.append('description', (document.getElementById('upload-description') as HTMLTextAreaElement).value);
        body.append('emoji', emoji.value);
        body.append('start',String(clip.start));body.append('end',String(clip.end));body.append('volume',String(clip.volume));
        busy = true; fields.disabled = true; cancel.disabled = true; save.disabled = true;
        next.disabled=true;back.disabled=true;
        hidePicker(); error.textContent = 'Uploading and preparing audio…'; save.setAttribute('aria-busy', 'true');
        save.innerHTML = '<span class="switch-spinner" aria-hidden="true"></span> Uploading…';
        try {
            const response = await fetch('/upload-sound', {method: 'POST', headers: {'X-CSRF-Token': csrf}, body, signal: AbortSignal.timeout(120000)});
            if (!response.ok) {
                const message = (await response.text()).trim().slice(0,300) || 'Upload failed. Please try again.';
                if(response.status===401||response.status===403)await loadAccount();
                throw new Error(message);
            }
            const result = await response.json() as {name: string};
            const status = document.getElementById('upload-status')!;
            status.textContent = `${result.name} added to the library. Use + or "Choose slot" to upload it to Discord.`; status.hidden = false;
            form.reset(); emoji.value='';syncEmojiChoice(); automaticName = ''; showStep(0); dialog.close();
        } catch (failure) {error.textContent = failure instanceof Error ? failure.message : 'Upload failed. Please try again.';}
        finally {busy = false; fields.disabled = false; cancel.disabled = false; save.disabled = !canUpload; save.removeAttribute('aria-busy'); save.textContent = 'Add to library';back.disabled=false;next.disabled=!editor.valid();}
    });
    subscribeDiscordSession(session => {
        if (dialog.hasAttribute('open')) {
            applyAccount(session);
            if (!session.user) releaseAudio();
        }
    });
    return open;
};
