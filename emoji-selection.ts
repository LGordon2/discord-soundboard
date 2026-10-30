export const renderEmojiSelection = (button: HTMLElement, value: string) => {
    button.replaceChildren();
    const label=document.createElement('span');
    label.textContent=value?'Change emoji':'Choose emoji';
    if(value){
        const icon=document.createElement('span');icon.setAttribute('aria-hidden','true');
        const id=/^[1-9][0-9]{0,19}$/.test(value)?value:value.match(/^<a?:[A-Za-z0-9_]{1,32}:([1-9][0-9]{0,19})>$/)?.[1];
        if(id){const image=document.createElement('img');image.src=`https://cdn.discordapp.com/emojis/${id}.webp?size=32`;image.alt='';image.width=20;image.height=20;icon.append(image);}
        else icon.textContent=value;
        button.append(icon);
    }
    button.append(label);
    button.setAttribute('aria-label',value?`Change emoji (${/^\d+$/.test(value)?'custom emoji':value})`:'Choose emoji');
};
