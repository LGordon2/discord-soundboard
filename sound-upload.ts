import { customEmojiChoices } from './sound-metadata';

export const initSoundUpload = () => {
    const dialog = document.getElementById('sound-upload') as HTMLDialogElement;
    const form = document.getElementById('sound-upload-form') as HTMLFormElement;
    const fields = document.getElementById('sound-upload-fields') as HTMLFieldSetElement;
    const file = document.getElementById('upload-file') as HTMLInputElement;
    const name = document.getElementById('upload-name') as HTMLInputElement;
    const password = document.getElementById('upload-password') as HTMLInputElement;
    const audio = document.getElementById('upload-audio-preview') as HTMLAudioElement;
    const emoji = document.getElementById('upload-emoji') as HTMLInputElement;
    const picker = document.getElementById('upload-emoji-picker')!;
    const choose = document.getElementById('upload-choose-emoji')!;
    const cancel = document.getElementById('cancel-sound-upload') as HTMLButtonElement;
    const save = document.getElementById('save-sound-upload') as HTMLButtonElement;
    const error = document.getElementById('upload-error')!;
    let busy = false;
    let objectURL = '';
    let automaticName = '';
    let pickerVersion = 0;
    const hidePicker = () => {pickerVersion++; picker.hidden = true; picker.replaceChildren(); choose.setAttribute('aria-expanded', 'false');};
    const releaseAudio = () => {
        audio.pause?.(); audio.removeAttribute('src'); audio.load?.(); audio.hidden = true;
        if (objectURL) URL.revokeObjectURL(objectURL);
        objectURL = '';
    };
    const open = () => {
        if (!document.body.classList.contains('ui-preview')) return;
        error.textContent = '';
        if (!dialog.hasAttribute('open')) dialog.showModal();
        if (file.files?.[0] && !objectURL) file.dispatchEvent(new Event('change'));
        file.focus();
    };
    cancel.addEventListener('click', () => {if (!busy) dialog.close();});
    dialog.addEventListener('cancel', event => {if (busy) event.preventDefault();});
    dialog.addEventListener('close', () => {releaseAudio(); hidePicker();});
    window.addEventListener('pagehide', releaseAudio);
    file.addEventListener('change', () => {
        releaseAudio(); error.textContent = '';
        const selected = file.files?.[0]; if (!selected) return;
        if (selected.size > 10 * 1024 * 1024) {error.textContent = 'Choose a file under 10 MB.'; return;}
        if (!name.value || name.value === automaticName) {
            automaticName = Array.from(selected.name.replace(/\.[^.]+$/, '').replace(/[\\/\x00-\x1f]/g, ' ').trim()).slice(0, 32).join('');
            name.value = automaticName;
        }
        objectURL = URL.createObjectURL(selected); audio.src = objectURL; audio.hidden = false;
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
            mountEmojiPicker(picker, customEmojiChoices(), value => {emoji.value = value; hidePicker(); choose.focus();});
        } catch {if (version === pickerVersion) picker.textContent = 'Could not load emoji. You can paste one below.';}
    });
    form.addEventListener('submit', async event => {
        event.preventDefault(); if (busy || !document.body.classList.contains('ui-preview')) return;
        const selected = file.files?.[0];
        if (!selected || !password.value) {error.textContent = 'Choose an audio file and enter the upload password.'; return;}
        if (selected.size > 10 * 1024 * 1024) {error.textContent = 'Choose a file under 10 MB.'; return;}
        const body = new FormData();
        body.append('file', selected); body.append('name', name.value.trim());
        body.append('description', (document.getElementById('upload-description') as HTMLTextAreaElement).value);
        body.append('emoji', emoji.value);
        busy = true; fields.disabled = true; cancel.disabled = true; save.disabled = true;
        hidePicker(); error.textContent = 'Uploading and preparing audio…'; save.setAttribute('aria-busy', 'true');
        save.innerHTML = '<span class="switch-spinner" aria-hidden="true"></span> Uploading…';
        try {
            const response = await fetch('/upload-sound', {method: 'POST', headers: {'X-Upload-Password': password.value}, body, signal: AbortSignal.timeout(120000)});
            if (!response.ok) throw new Error((await response.text()).trim().slice(0, 300) || 'Upload failed. Please try again.');
            const result = await response.json() as {name: string};
            const status = document.getElementById('upload-status')!;
            status.textContent = `${result.name} added to the library. Use + or "Choose slot" to upload it to Discord.`; status.hidden = false;
            const rememberedPassword = password.value;
            form.reset(); password.value = rememberedPassword; automaticName = ''; dialog.close();
        } catch (failure) {error.textContent = failure instanceof Error ? failure.message : 'Upload failed. Please try again.';}
        finally {busy = false; fields.disabled = false; cancel.disabled = false; save.disabled = false; save.removeAttribute('aria-busy'); save.textContent = 'Add to library';}
    });
    return open;
};
