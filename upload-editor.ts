export type ClipSelection = {start: number; end: number; volume: number};

export const createUploadEditor = (audio: HTMLAudioElement, changed: (valid: boolean) => void, stopOthers: () => void) => {
    const start = document.getElementById('clip-start') as HTMLInputElement;
    const end = document.getElementById('clip-end') as HTMLInputElement;
    const volume = document.getElementById('clip-volume') as HTMLInputElement;
    const preview = document.getElementById('preview-upload-clip') as HTMLButtonElement;
    const status = document.getElementById('clip-status')!;
    const timeline = document.getElementById('upload-timeline')!;
    const canvas = document.getElementById('upload-waveform') as HTMLCanvasElement;
    const region = document.getElementById('upload-clip-region')!;
    let duration = 0;
    let generation = 0;
    let file: File | undefined;
    let playing = false;
    let playVersion = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let context: AudioContext | undefined;
    let gain: GainNode | undefined;
    const selection = (): ClipSelection => ({start:Number(start.value),end:Number(end.value),volume:Number(volume.value)/100});
    const valid = () => {const clip=selection();return duration>0&&clip.start>=0&&clip.end>clip.start&&clip.end<=duration+.001&&clip.end-clip.start<=5.001;};
    const stop = () => {playVersion++;clearTimeout(timer);audio.pause?.();playing=false;preview.textContent='Preview clip';preview.setAttribute('aria-pressed','false');};
    const applyVolume = () => {
        const value=selection().volume;
        if (gain && context) {audio.volume=1;gain.gain.setValueAtTime(value,context.currentTime);}
        else audio.volume=value;
        document.getElementById('clip-volume-value')!.textContent=`${Math.round(value*100)}%`;
    };
    const update = () => {
        const clip=selection();
        document.getElementById('clip-start-value')!.textContent=`${clip.start.toFixed(2)} s`;
        document.getElementById('clip-end-value')!.textContent=`${clip.end.toFixed(2)} s`;
        region.style.left=`${duration?clip.start/duration*100:0}%`;
        region.style.width=`${duration?(clip.end-clip.start)/duration*100:0}%`;
        if (duration) status.textContent=`${(clip.end-clip.start).toFixed(2)} s selected of ${duration.toFixed(2)} s. Maximum 5 seconds.`;
        preview.disabled=!valid();applyVolume();changed(valid());
    };
    const waveform = async (source: File, version: number) => {
        const drawing=canvas.getContext?.('2d');
        if (!drawing || !window.OfflineAudioContext) return;
        try {
            const decoder=new OfflineAudioContext(1,1,8000);
            const buffer=await decoder.decodeAudioData(await source.arrayBuffer());
            if (version!==generation) return;
            const width=canvas.width,height=canvas.height;
            drawing.clearRect(0,0,width,height);drawing.fillStyle='#0284c7';
            const channels=Array.from({length:Math.min(buffer.numberOfChannels,8)},(_,index)=>buffer.getChannelData(index)),bars=256;
            const length=buffer.length;
            for(let bar=0;bar<bars;bar++) {
                const from=Math.floor(bar*length/bars),to=Math.floor((bar+1)*length/bars);
                const stride=Math.max(1,Math.floor((to-from)/64));let peak=0;
                for(const samples of channels)for(let i=from;i<to;i+=stride)peak=Math.max(peak,Math.abs(samples[i]||0));
                const size=Math.max(2,Math.min(1,peak)*height);
                drawing.fillRect(bar*width/bars,(height-size)/2,Math.max(1,width/bars-1),size);
            }
        } catch { /* The range controls also work if waveform decoding is unavailable. */ }
    };
    audio.addEventListener('loadedmetadata', () => {
        if (!file) return;
        const seconds=audio.duration;
        if (!Number.isFinite(seconds)||seconds<=0||seconds>120) {duration=0;status.textContent='Choose audio no longer than two minutes.';changed(false);return;}
        duration=seconds;start.max=String(Math.max(0,duration-.01));end.max=String(duration);
        start.value='0';end.value=String(Math.min(5,duration));start.disabled=false;end.disabled=false;
        update();void waveform(file,generation);
    });
    audio.addEventListener('error',()=>{duration=0;stop();status.textContent='Could not read this audio. Try MP3 or WAV in this browser.';changed(false);});
    audio.addEventListener('timeupdate',()=>{if(playing&&audio.currentTime>=selection().end)stop();});
    audio.addEventListener('ended',stop);
    audio.addEventListener('playing',()=>{if(playing){clearTimeout(timer);timer=setTimeout(stop,Math.max(0,selection().end-audio.currentTime)*1000);}});
    start.addEventListener('input',()=>{stop();const value=Number(start.value);if(Number(end.value)<=value||Number(end.value)-value>5)end.value=String(Math.min(duration,value+5));update();});
    end.addEventListener('input',()=>{stop();const value=Number(end.value);if(value<=Number(start.value)||value-Number(start.value)>5)start.value=String(Math.max(0,value-5));update();});
    volume.addEventListener('input',applyVolume);
    timeline.addEventListener('click',event=>{
        if(!duration||start.disabled)return;
        const rect=timeline.getBoundingClientRect();if(!rect.width)return;
        stop();const point=Math.max(0,Math.min(duration-.01,(event.clientX-rect.left)/rect.width*duration));
        start.value=String(point);end.value=String(Math.min(duration,point+5));update();
    });
    preview.addEventListener('click',()=>{
        if(playing){stop();return;}if(!valid())return;stopOthers();
        const version=++playVersion;
        try {
            if(!context&&window.AudioContext){context=new AudioContext();const source=context.createMediaElementSource(audio);gain=context.createGain();source.connect(gain);gain.connect(context.destination);}
            if(context)void context.resume().catch(()=>{});
            applyVolume();audio.currentTime=selection().start;playing=true;preview.textContent='Stop preview';preview.setAttribute('aria-pressed','true');
            void audio.play().catch(()=>{if(version!==playVersion)return;stop();status.textContent='Could not play this clip. Try again.';});
        } catch {stop();status.textContent='Could not play this clip. Try again.';}
    });
    return {
        selection,valid,stop,
        load(source: File) {stop();generation++;file=source;duration=0;start.value='0';end.value='0';volume.value='100';start.disabled=true;end.disabled=true;preview.disabled=true;status.textContent='Reading audio…';region.style.width='0%';canvas.getContext?.('2d')?.clearRect(0,0,canvas.width,canvas.height);changed(false);},
        clear() {stop();generation++;file=undefined;duration=0;changed(false);if(context)void context.suspend().catch(()=>{});},
    };
};
