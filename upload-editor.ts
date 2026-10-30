import { fitClipWindow, resizeClip, moveClip } from './clip-timeline';
import type { ClipRange } from './clip-timeline';

export type ClipSelection = {start: number; end: number; volume: number};

export const createUploadEditor = (audio: HTMLAudioElement, changed: (valid: boolean) => void, stopOthers: () => void) => {
    const start = document.getElementById('clip-start') as HTMLInputElement;
    const end = document.getElementById('clip-end') as HTMLInputElement;
    const volume = document.getElementById('clip-volume') as HTMLInputElement;
    const preview = document.getElementById('preview-upload-clip') as HTMLButtonElement;
    const status = document.getElementById('clip-status')!;
    const timeline = document.getElementById('upload-timeline')!;
    const viewport = document.getElementById('upload-waveform-viewport')!;
    const grabStart = document.getElementById('clip-grab-start') as HTMLButtonElement;
    const grabEnd = document.getElementById('clip-grab-end') as HTMLButtonElement;
    const move = document.getElementById('clip-move') as HTMLButtonElement;
    const position = document.getElementById('clip-position') as HTMLInputElement;
    const fit = document.getElementById('clip-fit') as HTMLButtonElement;
    const full = document.getElementById('clip-show-all') as HTMLButtonElement;
    const canvas = document.getElementById('upload-waveform') as HTMLCanvasElement;
    const region = document.getElementById('upload-clip-region')!;
    let duration = 0;
    let view: ClipRange = {start:0,end:0};
    let fitMode = true;
    let peaks: Float32Array | undefined;
    let drawn = '';
    let drag: {id:number;kind:'start'|'end'|'move';x:number;width:number;clip:ClipRange;view:ClipRange} | undefined;
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
    const draw = () => {
        const drawing=canvas.getContext?.('2d');
        if(!drawing||!duration)return;
        const signature=`${view.start}:${view.end}`;
        if(drawn===signature)return;
        drawn=signature;drawing.clearRect(0,0,canvas.width,canvas.height);drawing.fillStyle='#0284c7';
        const bars=256;
        for(let bar=0;bar<bars;bar++) {
            let peak=0;
            if(peaks) {
                const from=Math.max(0,Math.floor((view.start+(view.end-view.start)*bar/bars)/duration*peaks.length));
                const to=Math.min(peaks.length,Math.max(from+1,Math.ceil((view.start+(view.end-view.start)*(bar+1)/bars)/duration*peaks.length)));
                for(let index=from;index<to;index++)peak=Math.max(peak,peaks[index]||0);
            }
            const size=Math.max(2,peak*canvas.height);
            drawing.fillRect(bar*canvas.width/bars,(canvas.height-size)/2,Math.max(1,canvas.width/bars-1),size);
        }
    };
    const update = (autoFit = true) => {
        const clip=selection();
        if(autoFit&&!drag&&duration) {fitMode=true;view=fitClipWindow(clip,duration);}
        const span=view.end-view.start;
        document.getElementById('clip-start-value')!.textContent=`${clip.start.toFixed(2)} s`;
        document.getElementById('clip-end-value')!.textContent=`${clip.end.toFixed(2)} s`;
        region.style.left=`${span?(clip.start-view.start)/span*100:0}%`;
        region.style.width=`${span?(clip.end-clip.start)/span*100:0}%`;
        if(duration)status.textContent=`${(clip.end-clip.start).toFixed(2)} s selected of ${duration.toFixed(2)} s. Maximum 5 seconds.`;
        const enabled=valid();
        preview.disabled=grabStart.disabled=grabEnd.disabled=move.disabled=!enabled;
        full.disabled=fit.disabled=!duration;
        const minimum=Math.min(.01,duration);
        for(const [handle,value,min,max] of [[grabStart,clip.start,Math.max(0,clip.end-5),clip.end-minimum],[grabEnd,clip.end,clip.start+minimum,Math.min(duration,clip.start+5)]] as const) {
            handle.setAttribute('aria-valuemin',String(min));handle.setAttribute('aria-valuemax',String(max));handle.setAttribute('aria-valuenow',String(value));handle.setAttribute('aria-valuetext',`${value.toFixed(2)} seconds`);
        }
        position.max=String(Math.max(0,duration-(clip.end-clip.start)));position.value=String(clip.start);position.disabled=!enabled||duration<=clip.end-clip.start;
        document.getElementById('clip-view-label')!.textContent=duration?`${view.start.toFixed(2)}–${view.end.toFixed(2)} s`:'';
        fit.setAttribute('aria-pressed',String(fitMode));full.setAttribute('aria-pressed',String(!fitMode));
        applyVolume();changed(enabled);draw();
    };
    const setClip = (clip: ClipRange, autoFit = true) => {
        start.value=String(clip.start);end.value=String(clip.end);update(autoFit);
    };
    const waveform = async (source: File, version: number) => {
        if(!canvas.getContext?.('2d')||!window.OfflineAudioContext)return;
        try {
            const decoder=new OfflineAudioContext(1,1,8000);
            const buffer=await decoder.decodeAudioData(await source.arrayBuffer());
            if(version!==generation)return;
            const channels=Array.from({length:Math.min(buffer.numberOfChannels,8)},(_,index)=>buffer.getChannelData(index));
            const bins=new Float32Array(8192);
            for(let bin=0;bin<bins.length;bin++) {
                const from=Math.floor(bin*buffer.length/bins.length),to=Math.floor((bin+1)*buffer.length/bins.length);
                const stride=Math.max(1,Math.floor((to-from)/32));let peak=0;
                for(const samples of channels)for(let index=from;index<to;index+=stride)peak=Math.max(peak,Math.abs(samples[index]||0));
                bins[bin]=Math.min(1,peak);
            }
            peaks=bins;drawn='';draw();
        } catch { /* Trimming remains usable if waveform decoding is unavailable. */ }
    };
    const finishDrag = () => {
        if(!drag)return;
        const id=drag.id;drag=undefined;timeline.classList.remove('is-trimming');
        if(timeline.hasPointerCapture?.(id))timeline.releasePointerCapture(id);
        update();
    };
    const resetTimeline = () => {
        if(drag) {const id=drag.id;drag=undefined;if(timeline.hasPointerCapture?.(id))timeline.releasePointerCapture(id);}
        timeline.classList.remove('is-trimming');peaks=undefined;drawn='';view={start:0,end:0};fitMode=true;
        grabStart.disabled=grabEnd.disabled=move.disabled=position.disabled=full.disabled=fit.disabled=true;
    };
    audio.addEventListener('loadedmetadata', () => {
        if (!file) return;
        const seconds=audio.duration;
        if (!Number.isFinite(seconds)||seconds<=0||seconds>120) {duration=0;status.textContent='Choose audio no longer than two minutes.';changed(false);return;}
        duration=Math.floor(seconds*100)/100||seconds;end.min=String(Math.min(.01,duration));start.max=String(Math.max(0,duration-.01));end.max=String(duration);
        start.value='0';end.value=String(Math.min(5,duration));start.disabled=false;end.disabled=false;
        update();void waveform(file,generation);
    });
    audio.addEventListener('error',()=>{resetTimeline();duration=0;stop();region.style.width='0%';status.textContent='Could not read this audio. Try MP3 or WAV in this browser.';changed(false);});
    audio.addEventListener('timeupdate',()=>{if(playing&&audio.currentTime>=selection().end)stop();});
    audio.addEventListener('ended',stop);
    audio.addEventListener('playing',()=>{if(playing){clearTimeout(timer);timer=setTimeout(stop,Math.max(0,selection().end-audio.currentTime)*1000);}});
    start.addEventListener('input',()=>{stop();const value=Number(start.value);if(Number(end.value)<=value||Number(end.value)-value>5)end.value=String(Math.min(duration,value+5));update();});
    end.addEventListener('input',()=>{stop();const value=Number(end.value);if(value<=Number(start.value)||value-Number(start.value)>5)start.value=String(Math.max(0,value-5));update();});
    volume.addEventListener('input',applyVolume);
    const editable = () => duration>0&&!start.disabled&&!(start.closest('fieldset') as HTMLFieldSetElement | null)?.disabled;
    timeline.addEventListener('pointerdown',event=>{
        if(!editable()||(event.button!==undefined&&event.button!==0)||event.isPrimary===false)return;
        const rect=viewport.getBoundingClientRect();if(!rect.width)return;
        event.preventDefault();stop();
        const target=event.target instanceof Element?event.target:null;
        const kind=target?.closest('#clip-grab-start')?'start':target?.closest('#clip-grab-end')?'end':'move';
        (kind==='start'?grabStart:kind==='end'?grabEnd:move).focus({preventScroll:true});
        if(!target?.closest('#upload-clip-region')) {
            const point=view.start+(event.clientX-rect.left)/rect.width*(view.end-view.start);
            setClip(moveClip(selection(),Math.round((point-(selection().end-selection().start)/2)*100)/100,duration),false);
        }
        drag={id:event.pointerId,kind,x:event.clientX,width:rect.width,clip:selection(),view:{...view}};
        timeline.classList.add('is-trimming');timeline.setPointerCapture?.(event.pointerId);
    });
    timeline.addEventListener('pointermove',event=>{
        if(!drag||event.pointerId!==drag.id)return;
        event.preventDefault();
        const delta=(event.clientX-drag.x)/drag.width*(drag.view.end-drag.view.start);
        const target=Math.round((drag.kind==='move'?drag.clip.start+delta:drag.clip[drag.kind]+delta)*100)/100;
        setClip(drag.kind==='move'?moveClip(drag.clip,target,duration):resizeClip(drag.clip,drag.kind,target,duration),false);
    });
    for(const name of ['pointerup','pointercancel','lostpointercapture'] as const)timeline.addEventListener(name,event=>{if(drag&&event.pointerId===drag.id)finishDrag();});
    for(const [button,edge] of [[grabStart,'start'],[grabEnd,'end'],[move,'move']] as const)button.addEventListener('keydown',event=>{
        if(!editable())return;
        const amount=event.shiftKey ? .1 : .01;
        let value:number;
        const current=selection();const anchor=edge==='end'?current.end:current.start;
        if(event.key==='ArrowLeft'||event.key==='ArrowDown')value=anchor-amount;
        else if(event.key==='ArrowRight'||event.key==='ArrowUp')value=anchor+amount;
        else if(event.key==='Home')value=0;
        else if(event.key==='End')value=duration;
        else return;
        event.preventDefault();stop();setClip(edge==='move'?moveClip(current,value,duration):resizeClip(current,edge,value,duration));
    });
    position.addEventListener('input',()=>{if(editable()){stop();setClip(moveClip(selection(),Number(position.value),duration));}});
    full.addEventListener('click',()=>{if(duration){finishDrag();fitMode=false;view={start:0,end:duration};update(false);}});
    fit.addEventListener('click',()=>{if(duration){finishDrag();update();}});
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
        load(source: File) {stop();resetTimeline();generation++;file=source;duration=0;start.value='0';end.value='0';volume.value='100';start.disabled=true;end.disabled=true;preview.disabled=true;status.textContent='Reading audio…';region.style.width='0%';canvas.getContext?.('2d')?.clearRect(0,0,canvas.width,canvas.height);changed(false);},
        clear() {stop();resetTimeline();generation++;file=undefined;duration=0;changed(false);if(context)void context.suspend().catch(()=>{});},
    };
};
