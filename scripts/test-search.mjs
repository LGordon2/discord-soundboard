import assert from 'node:assert/strict';
import { withinEditDistance, createSoundSearch } from '../sound-search.ts';

for (const [left, right, budget, expected] of [
    ['sound', 'sound', 0, true],
    ['funy', 'funny', 1, true],
    ['soundd', 'sound', 1, true],
    ['carton', 'cartoon', 1, true],
    ['kitten', 'sitting', 2, false],
    ['kitten', 'sitting', 3, true],
    ['x', 'a very long name', 2, false],
    ['', '', 0, true],
]) assert.equal(withinEditDistance(left, right, budget), expected, `${left}/${right}`);

assert.equal(createSoundSearch('funnny')('Funny'), true);
assert.equal(createSoundSearch('carton')('Name', 'A cartoon quote'), true);
assert.equal(createSoundSearch('MOVIE QUOTE')('Movie', 'A famous quote'), true);
assert.equal(createSoundSearch('cafe')('Café'), true);
assert.equal(createSoundSearch('tv')('Other', 'A TV quote'), true);
assert.equal(createSoundSearch('tx')('Other', 'A TV quote'), false);
assert.equal(createSoundSearch('totally unrelated')('Funny', 'A cartoon quote'), false);
assert.equal(createSoundSearch('😂')('Funny', 'A cartoon quote'), false);
assert.equal(createSoundSearch('')('Anything'), true);
console.log('PASS: bounded Levenshtein, name/description matching, multiple words, accents, and strict short queries');
