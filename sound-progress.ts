const active = new WeakMap<HTMLElement, () => void>();
export const stopSoundProgress = (card: HTMLElement) => {active.get(card)?.();};

const mount = (card: HTMLElement) => {
    const track = document.createElement('div');
    track.className = 'sound-playback-progress';
    track.setAttribute('aria-hidden', 'true');
    const fill = document.createElement('span');
    track.append(fill);card.append(track);card.classList.add('sound-playing');
    return {
        update: (fraction: number) => {fill.style.transform=`scaleX(${Number.isFinite(fraction)?Math.max(0,Math.min(1,fraction)):0})`;},
        remove: () => {track.remove();card.classList.remove('sound-playing');},
    };
};

// Follow the media clock, including stalls and seeks, rather than a CSS timer.
export const trackSoundProgress = (card: HTMLElement, audio: HTMLAudioElement) => {
    stopSoundProgress(card);
    let view: ReturnType<typeof mount> | undefined;
    let frame: number | undefined;
    let stopped = false;
    const update = () => {if(view)view.update(audio.duration>0?audio.currentTime/audio.duration:0);};
    const tick = () => {
        frame=undefined;
        if(stopped)return;
        if(!card.isConnected){cleanup();return;}
        update();
        if(window.requestAnimationFrame)frame=window.requestAnimationFrame(tick);
    };
    const start = () => {
        if(stopped)return;
        if(!view){view=mount(card);active.set(card,cleanup);}
        update();
        if(frame===undefined && window.requestAnimationFrame)frame=window.requestAnimationFrame(tick);
    };
    const cleanup = () => {
        if(stopped)return;stopped=true;
        if(frame!==undefined)window.cancelAnimationFrame(frame);
        for(const event of ['playing','timeupdate','durationchange','ended','pause','error'])audio.removeEventListener(event,handlers[event]!);
        view?.remove();
        if(active.get(card)===cleanup)active.delete(card);
    };
    const handlers: Record<string, () => void> = {playing:start,timeupdate:update,durationchange:update,ended:cleanup,pause:cleanup,error:cleanup};
    for(const [event,handler] of Object.entries(handlers))audio.addEventListener(event,handler);
    active.set(card,cleanup);
};

// Discord doesn't expose a remote playback clock; use the known clip duration.
export const estimateSoundProgress = (card: HTMLElement, duration: number) => {
    if(!Number.isFinite(duration)||duration<=0)return;
    stopSoundProgress(card);
    const view=mount(card);
    const began=performance.now();
    let frame:number|undefined;
    let stopped=false;
    const cleanup=()=>{
        if(stopped)return;stopped=true;
        clearTimeout(timer);
        if(frame!==undefined)window.cancelAnimationFrame(frame);
        view.remove();
        if(active.get(card)===cleanup)active.delete(card);
    };
    const tick=()=>{
        frame=undefined;
        if(stopped)return;
        if(!card.isConnected){cleanup();return;}
        view.update((performance.now()-began)/(duration*1000));
        if(window.requestAnimationFrame)frame=window.requestAnimationFrame(tick);
    };
    const timer=setTimeout(cleanup,duration*1000);
    active.set(card,cleanup);view.update(0);
    if(window.requestAnimationFrame)frame=window.requestAnimationFrame(tick);
};
