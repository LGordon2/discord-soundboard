#!/bin/bash

find -name '*.ogg' | sed s/.ogg$/.mp3/ | xargs -I {} make -j4 "{}"