const normalize = (value: string) => value.normalize('NFKD').replace(/\p{M}/gu, '').toLocaleLowerCase();

// Bounded Levenshtein: reject different lengths and stop as soon as a row
// cannot fall within the small typo budget. No full distance matrix is stored.
export const withinEditDistance = (left: string, right: string, limit: number): boolean => {
    if (Math.abs(left.length - right.length) > limit) return false;
    let previous = Array.from({ length: right.length + 1 }, (_, i) => i);
    for (let i = 1; i <= left.length; i++) {
        const row = [i];
        let minimum = i;
        for (let j = 1; j <= right.length; j++) {
            const distance = Math.min(
                (previous[j] ?? 0) + 1,
                (row[j - 1] ?? 0) + 1,
                (previous[j - 1] ?? 0) + (left[i - 1] === right[j - 1] ? 0 : 1),
            );
            row[j] = distance;
            minimum = Math.min(minimum, distance);
        }
        if (minimum > limit) return false;
        previous = row;
    }
    return (previous[right.length] ?? 0) <= limit;
};

export const createSoundSearch = (query: string) => {
    const normalizedQuery = normalize(query).trim();
    const queryWords = normalizedQuery.match(/[\p{L}\p{N}]+/gu) || [];
    return (name: string, description = '') => {
        const text = normalize(`${name} ${description}`);
        if (!normalizedQuery || text.includes(normalizedQuery)) return true;
        // Emoji/punctuation searches remain literal instead of matching all rows.
        if (queryWords.length === 0) return false;
        const words = text.match(/[\p{L}\p{N}]+/gu) || [];
        return queryWords.every(term => {
            const budget = term.length >= 7 ? 2 : term.length >= 4 ? 1 : 0;
            return words.some(word => word.includes(term) ||
                (budget > 0 && withinEditDistance(term, word, budget)));
        });
    };
};
