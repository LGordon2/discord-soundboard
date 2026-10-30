import assert from 'node:assert/strict';
import {isSingleEmojiValue} from '../single-emoji.ts';
for(const value of ['', ' 🦆 ', '☕️','1️⃣','🇺🇸','👍🏽','👨‍👩‍👧‍👦','👩🏽‍🚀','🏳️‍🌈','12345','<:party:12345>','<a:party:12345>'])assert.equal(isSingleEmojiValue(value),true,value);
for(const value of ['🦆🎉','👩🏽‍🚀👍🏽','🇺🇸🇨🇦','1️⃣2️⃣','<:a:12345><:b:67890>','hello','18446744073709551616'])assert.equal(isSingleEmojiValue(value),false,value);
console.log('PASS: one-emoji input validation preserves combined emoji and rejects multiple/custom overflow');
