# Define the rule to convert .ogg to .mp3
%.mp3: %.ogg
	ffmpeg -i "$<" "$@"