export type ClipRange = {start: number; end: number};
const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value));

export const fitClipWindow = (clip: ClipRange, duration: number): ClipRange => {
    if (duration <= 0) return {start: 0, end: 0};
    const span = Math.min(duration, Math.max(.02, (clip.end - clip.start) * 2));
    const start = clamp((clip.start + clip.end - span) / 2, 0, duration - span);
    return {start, end: start + span};
};
export const resizeClip = (clip: ClipRange, edge: 'start' | 'end', time: number, duration: number): ClipRange => {
    const minimum = Math.min(.01, duration);
    return edge === 'start' ? {start: clamp(time, Math.max(0, clip.end - 5), clip.end - minimum), end: clip.end} :
        {start: clip.start, end: clamp(time, clip.start + minimum, Math.min(duration, clip.start + 5))};
};
export const moveClip = (clip: ClipRange, targetStart: number, duration: number): ClipRange => {
    const length = clip.end - clip.start;
    const start = clamp(targetStart, 0, Math.max(0, duration - length));
    return {start, end: start + length};
};
