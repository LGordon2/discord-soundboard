import assert from 'node:assert/strict';
import {fitClipWindow,resizeClip,moveClip} from '../clip-timeline.ts';
const close=(a,b)=>assert.ok(Math.abs(a-b)<1e-8,`${a} != ${b}`);
assert.deepEqual(fitClipWindow({start:0,end:5},120),{start:0,end:10});
assert.deepEqual(fitClipWindow({start:10,end:11},120),{start:9.5,end:11.5});
assert.deepEqual(fitClipWindow({start:119,end:120},120),{start:118,end:120});
assert.deepEqual(resizeClip({start:2,end:4},'start',-100,120),{start:0,end:4});
assert.deepEqual(resizeClip({start:2,end:4},'end',1000,120),{start:2,end:7});
assert.deepEqual(moveClip({start:2,end:4},119,120),{start:118,end:120});
let seed=42;
const random=()=>{seed=(1664525*seed+1013904223)>>>0;return seed/2**32};
for(const duration of [.005,.05,1,5,20,120])for(let i=0;i<200;i++) {
 const minimum=Math.min(.01,duration),length=minimum+random()*(Math.min(5,duration)-minimum);
 const clip={start:random()*(duration-length),end:0};clip.end=clip.start+length;
 const view=fitClipWindow(clip,duration);
 assert.ok(view.start>=-1e-8&&view.end<=duration+1e-8);
 assert.ok(view.start<=clip.start+1e-8&&view.end>=clip.end-1e-8,'zoom must retain both grabbers');
 for(const target of [-100,0,random()*duration,duration,1000]) {
  const moved=moveClip(clip,target,duration);close(moved.end-moved.start,length);
  assert.ok(moved.start>=-1e-8&&moved.end<=duration+1e-8);
  for(const edge of ['start','end']) {
   const resized=resizeClip(clip,edge,target,duration);
   assert.ok(resized.start>=-1e-8&&resized.end<=duration+1e-8);
   assert.ok(resized.end-resized.start>=minimum-1e-8&&resized.end-resized.start<=5+1e-8);
   close(resized[edge==='start'?'end':'start'],clip[edge==='start'?'end':'start']);
  }
 }
}
console.log('PASS: automatic clip zoom bounds, edge constraints, whole-clip movement, and short-source invariants');
