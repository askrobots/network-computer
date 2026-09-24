#!/bin/sh
# Benchmark whisper.cpp on a desk (run as root on it; writes /root/whisper-bench.log).
# Results from 2026-09-23 are in docs/DESKTOP.md.
set -e
exec > /root/whisper-bench.log 2>&1
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=600 install -y -q build-essential cmake git >/dev/null
cd /opt
[ -d whisper.cpp ] || git clone -q --depth 1 https://github.com/ggml-org/whisper.cpp
cd whisper.cpp
cmake -B build -DCMAKE_BUILD_TYPE=Release >/dev/null
cmake --build build -j 4 --config Release >/dev/null
for m in tiny.en base.en small.en; do sh ./models/download-ggml-model.sh $m >/dev/null 2>&1; done
espeak-ng -s 150 -w /tmp/w.wav "Open Firefox and search for the weather in Chicago tomorrow."
ffmpeg -loglevel error -y -i /tmp/w.wav -ac 1 -ar 16000 /tmp/w16.wav
echo "clip: $(ffprobe -v error -show_entries format=duration -of csv=p=0 /tmp/w16.wav) s"
for m in tiny.en base.en small.en; do
  for t in 4; do
    s=$(date +%s.%N)
    txt=$(./build/bin/whisper-cli -m models/ggml-$m.bin -f /tmp/w16.wav -t $t -nt -np 2>/dev/null | tr -s ' \n' ' ')
    e=$(date +%s.%N)
    echo "$m threads=$t: $(echo "$e - $s" | bc) s  ->$txt"
  done
done
echo DONE
