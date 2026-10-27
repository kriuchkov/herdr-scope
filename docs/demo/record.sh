#!/bin/sh
# Records scope.gif from demo.html: one headless Chrome shot per frame, at
# twice the size so the text stays sharp, then a GIF at the spinner's 150 ms
# step. Needs Google Chrome (or CHROME=<binary>) and ffmpeg.
set -eu
cd "$(dirname "$0")"
chrome=${CHROME:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
frames=$(sed -n 's/.*FRAMES = \([0-9]*\).*/\1/p' demo.html)
# A cell is 9 by 20 pixels.
size=$(($(sed -n 's/.*COLS = \([0-9]*\).*/\1/p' demo.html) * 9)),$(($(sed -n 's/.*ROWS = \([0-9]*\).*/\1/p' demo.html) * 20))
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

seq 0 $((frames - 1)) | xargs -P 6 -I{} "$chrome" --headless=new --disable-gpu --hide-scrollbars \
	--allow-file-access-from-files --user-data-dir="$work/profile-{}" \
	--force-device-scale-factor=2 --window-size="$size" --virtual-time-budget=3000 \
	--screenshot="$work/frame-{}.png" "file://$PWD/demo.html?f={}" 2>/dev/null

ffmpeg -y -loglevel error -framerate 20/3 -start_number 0 -i "$work/frame-%d.png" \
	-vf "split[a][b];[a]palettegen=max_colors=128:stats_mode=full[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" \
	-loop 0 scope.gif
ls -l scope.gif
