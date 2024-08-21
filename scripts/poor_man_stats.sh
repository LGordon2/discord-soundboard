#!/bin/bash

TMP_FILE=$(mktemp)
journalctl -u discord-soundboard.service -o json > $TMP_FILE
jq -r .MESSAGE $TMP_FILE | grep GUILD_SOUNDBOARD_SOUND_CREATE | grep -Eio '("|\s)name"?:"?[!A-Za-z0-9 _-]+("|\s)' | sed s/\"//g | sed 's/^[ \t]*//' | sed 's/[ \t]*$//' | sort | uniq -c | sort -nr