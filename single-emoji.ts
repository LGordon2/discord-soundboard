export const singleEmojiMessage = 'Choose one emoji or one Discord custom emoji; leave blank to remove.';
export const isSingleEmojiValue = (input: string): boolean => {
    const value=input.trim();
    if(!value)return true;
    const custom=value.match(/^<a?:[A-Za-z0-9_]{1,32}:([1-9][0-9]{0,19})>$/)?.[1] || (/^[1-9][0-9]{0,19}$/.test(value)?value:'');
    if(custom)return BigInt(custom)<=18446744073709551615n;
    if(!/[\p{Extended_Pictographic}\p{Regional_Indicator}\u20e3]/u.test(value))return false;
    // Older browsers defer grapheme validation to the server.
    if(typeof Intl.Segmenter!=='function')return true;
    const segments=new Intl.Segmenter(undefined,{granularity:'grapheme'}).segment(value);
    const iterator=segments[Symbol.iterator]();
    return !iterator.next().done && Boolean(iterator.next().done);
};
