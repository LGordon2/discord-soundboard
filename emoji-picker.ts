type Choice = {value: string; label: string; group: string; image?: string};
const groups: Record<string, string> = {
    Faces: '😀 grinning happy|😃 smile happy|😄 smile laugh|😁 grin teeth|😆 laughing|😅 sweat smile|🤣 rolling laughing rofl|😂 tears joy lol|🙂 smile|🙃 upside down|😉 wink|😊 blush|😇 angel innocent|🥰 love hearts|😍 heart eyes|🤩 star eyes|😘 kiss|😋 yummy|😛 tongue|😜 wink tongue|🤪 crazy zany|😎 cool sunglasses|🥳 party celebration|🥹 holding tears|🥲 smiling tear|😏 smirk|😒 unimpressed|🙄 eye roll|😬 grimace awkward|😮 surprised|😱 scream shocked|😳 embarrassed flushed|🥺 pleading puppy eyes|😢 crying sad|😭 sob crying|😤 huff angry|😠 angry|😡 rage|🤬 swearing|🤯 mind blown|🤔 thinking|🤨 skeptical|🧐 monocle|🤫 quiet shush|🤭 giggle|🫢 surprised gasp|🫡 salute|🫠 melting|🫥 invisible|😐 neutral|😑 expressionless|😶 speechless|🫣 peeking|🤐 zipped mouth|🤥 lying|😴 sleep|🥱 yawn|🤤 drool|😵 dizzy|🥴 woozy drunk|🤢 sick nauseous|🤮 vomit|🤧 sneeze|😷 mask|🤒 fever|🤕 injured|🤑 money rich|🤠 cowboy|😈 devil|👿 angry devil|💀 skull dead|☠️ skull bones|💩 poop|🤡 clown|👻 ghost|👽 alien|👾 space invader|🤖 robot|🎃 pumpkin|😺 happy cat|😸 grin cat|😹 laughing cat|😻 heart eyes cat|🙀 shocked cat',
    People: '👍 thumbs up yes|👎 thumbs down no|👏 clap applause|🙌 hooray raised hands|👋 wave hello goodbye|🤚 raised hand|✋ stop hand|👌 okay|🤌 pinched fingers|🤏 tiny small|✌️ peace victory|🤞 fingers crossed luck|🤟 love gesture|🤘 rock horns|🤙 call me|👈 point left|👉 point right|👆 point up|👇 point down|☝️ index up|🫵 point you|✊ fist|👊 punch|🤝 handshake|🙏 pray please thanks|💪 strong muscle|🖕 middle finger|🫶 heart hands|🧠 brain|👀 eyes look|👁️ eye|👂 ear|👄 mouth|💋 kiss mark|🦾 robot arm|🕺 dance|💃 dancing|🤷 shrug|🤦 facepalm|🧑‍🚀 astronaut|🧙 wizard|🦸 superhero|🧟 zombie',
    Animals: '🐶 dog|🐱 cat|🐭 mouse|🐹 hamster|🐰 rabbit bunny|🦊 fox|🐻 bear|🐼 panda|🐨 koala|🐯 tiger|🦁 lion|🐮 cow|🐷 pig|🐸 frog|🐵 monkey|🙈 see no evil monkey|🙉 hear no evil monkey|🙊 speak no evil monkey|🐔 chicken|🐧 penguin|🐦 bird|🦆 duck|🦅 eagle|🦉 owl|🦇 bat|🐺 wolf|🐴 horse|🦄 unicorn|🐝 bee|🦋 butterfly|🐛 bug|🪲 beetle|🐌 snail|🐢 turtle|🐍 snake|🐙 octopus|🦑 squid|🦀 crab|🦞 lobster|🐬 dolphin|🐳 whale|🦈 shark|🐟 fish|🦖 dinosaur|🐉 dragon|🦥 sloth|🦦 otter|🦔 hedgehog|🦝 raccoon|🦫 beaver',
    Food: '🍏 apple|🍎 red apple|🍊 orange|🍋 lemon|🍌 banana|🍉 watermelon|🍇 grapes|🍓 strawberry|🍒 cherries|🍑 peach|🍍 pineapple|🥑 avocado|🍆 eggplant|🥕 carrot|🌽 corn|🌶️ pepper hot|🍄 mushroom|🥔 potato|🍞 bread|🥐 croissant|🧀 cheese|🥚 egg|🥓 bacon|🍔 burger|🍟 fries|🍕 pizza|🌭 hot dog|🌮 taco|🌯 burrito|🍜 noodles ramen|🍝 pasta|🍣 sushi|🍿 popcorn|🍩 donut|🍪 cookie|🎂 birthday cake|🍰 cake|🍦 ice cream|🍫 chocolate|🍬 candy|☕ coffee|🍵 tea|🧋 bubble tea|🍺 beer|🍻 cheers beers|🥂 champagne cheers|🍷 wine|🍸 cocktail|🥃 whiskey',
    Activities: '🎉 celebration party|🎊 confetti|🎈 balloon|🎁 gift present|🏆 trophy winner|🥇 gold medal|🏅 medal|⚽ soccer football|🏀 basketball|🏈 football|⚾ baseball|🎾 tennis|🎯 target bullseye|🎳 bowling|🎮 game controller gaming|🕹️ joystick|🎲 dice|🧩 puzzle|♟️ chess|🎰 slot machine|🎭 theater masks drama|🎨 art paint|🎤 microphone karaoke|🎧 headphones music|🎵 music note|🎶 music notes|🥁 drums drumroll|🎸 guitar|🎺 trumpet|🎷 saxophone|🎻 violin|🎹 piano|🎬 movie clapper|📣 megaphone cheering|📢 loudspeaker|🔔 bell|🔊 loud volume sound|🔇 muted silence|📯 horn|🎪 circus',
    Objects: '🚗 car|🚓 police car|🚑 ambulance|🚒 fire engine|🚨 siren alarm|🚀 rocket|🛸 ufo spaceship|✈️ airplane|🚁 helicopter|🚂 train|🚢 ship|🛶 canoe|🏠 house|🏰 castle|🗿 moai stone face|🗽 liberty|🌋 volcano|🏝️ island|🌍 earth world|🌙 moon|☀️ sun|⭐ star|🌟 glow star|🌈 rainbow|⚡ lightning thunder|🔥 fire lit|💥 explosion boom|💫 dizzy stars|✨ sparkles|❄️ snowflake|🌊 wave|💨 wind dash|☔ rain umbrella|💧 water drop|🌹 rose|🌻 sunflower|🍀 lucky clover|🌲 tree|💐 flowers|💎 diamond gem|💰 money bag|💸 money flying|💵 dollar|🪙 coin|🔑 key|🔒 lock|🛎️ service bell|🕰️ clock|⏰ alarm clock|📱 phone|☎️ telephone|💻 computer|📸 camera|📺 television tv|📻 radio|💡 light bulb idea|🔦 flashlight|🔨 hammer|🪓 axe|⚔️ swords|💣 bomb|🧨 firecracker|🪄 magic wand|🧲 magnet|🧪 test tube|💊 pill|🧻 toilet paper|🚽 toilet|🪦 gravestone',
    Symbols: '❤️ red heart love|🧡 orange heart|💛 yellow heart|💚 green heart|💙 blue heart|💜 purple heart|🖤 black heart|🤍 white heart|🤎 brown heart|💔 broken heart|💕 two hearts|💯 hundred perfect|✅ check yes|❌ cross no|❎ cross mark|⚠️ warning|🚫 forbidden no|⛔ stop|❓ question|❗ exclamation|‼️ double exclamation|⁉️ question exclamation|🆘 sos help|🆗 okay|🆒 cool|🆕 new|🆓 free|🅰️ a blood|🅱️ b blood|💤 sleep zzz|🔞 adults only|♻️ recycle|🏳️ white flag|🏴 black flag|🏁 racing finish flag|🚩 red flag|🏴‍☠️ pirate|🏳️‍🌈 pride rainbow|🇺🇸 usa america|🇬🇧 uk britain|🇨🇦 canada|🇦🇺 australia|🇫🇷 france|🇩🇪 germany|🇯🇵 japan|🇮🇹 italy|🇪🇸 spain|🇲🇽 mexico|🇧🇷 brazil|🇰🇷 korea|🇺🇦 ukraine',
};
const standard: Choice[] = Object.entries(groups).flatMap(([group, rows]) => rows.split('|').map(row => {
    const space = row.indexOf(' ');
    return {value: row.slice(0, space), label: row.slice(space + 1), group};
}));
const toneable = new Set('👍 👎 👏 🙌 👋 🤚 ✋ 👌 🤌 🤏 ✌️ 🤞 🤟 🤘 🤙 👈 👉 👆 👇 ☝️ 🫵 ✊ 👊 🙏 💪 🖕 👂'.split(' '));

export const mountEmojiPicker = (root: HTMLElement, custom: Choice[], choose: (value: string) => void) => {
    const search = document.createElement('input');
    search.type = 'search'; search.placeholder = 'Search emoji…'; search.className = 'emoji-search';
    search.setAttribute('aria-label', 'Search emoji');
    const categories = document.createElement('select');
    categories.setAttribute('aria-label', 'Emoji category');
    for (const group of ['All', ...Object.keys(groups), ...(custom.length ? ['Custom'] : [])]) {
        const option = document.createElement('option'); option.value = group; option.textContent = group; categories.append(option);
    }
    const tones = document.createElement('select');
    tones.setAttribute('aria-label', 'Emoji skin tone');
    ['', '🏻', '🏼', '🏽', '🏾', '🏿'].forEach((tone, index) => {
        const option = document.createElement('option'); option.value = tone;
        option.textContent = index ? `🖐${tone}` : 'Default tone'; tones.append(option);
    });
    const filters = document.createElement('div'); filters.className = 'emoji-picker-filters'; filters.append(categories, tones);
    const grid = document.createElement('div'); grid.className = 'emoji-grid'; grid.setAttribute('role', 'group'); grid.setAttribute('aria-label', 'Emoji choices');
    const count = document.createElement('p'); count.className = 'details-help'; count.setAttribute('role', 'status');
    const entries = [...custom, ...standard];
    const render = () => {
        const words = search.value.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
        const matches = entries.filter(item => (categories.value === 'All' || item.group === categories.value) && words.every(word => `${item.label} ${item.value}`.toLocaleLowerCase().includes(word)));
        grid.replaceChildren(...matches.map(item => {
            const value = tones.value && toneable.has(item.value) ? item.value.replace(/\uFE0F/g, '') + tones.value : item.value;
            const button = document.createElement('button'); button.type = 'button'; button.title = item.label;
            button.setAttribute('aria-label', item.label); button.setAttribute('data-emoji-choice', value);
            if (item.image) {const image = document.createElement('img'); image.src = item.image; image.alt = ''; image.loading = 'lazy'; button.append(image);}
            else button.textContent = value;
            button.addEventListener('click', () => choose(value));
            return button;
        }));
        count.textContent = matches.length ? `${matches.length} emoji` : 'No matching emoji. Try another search or category.';
    };
    search.addEventListener('input', render); categories.addEventListener('change', render); tones.addEventListener('change', render);
    root.replaceChildren(search, filters, grid, count);
    // Select controls have their first option selected in browsers; set explicitly
    // so the state is also deterministic in DOM integration tests.
    categories.value = 'All'; tones.value = '';
    render(); search.focus();
};
