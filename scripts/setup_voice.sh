#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
voice_dir="${AGENTDECK_VOICE_DIR:-$HOME/.agentdeck-tools/voice}"
mkdir -p "$voice_dir"
chmod 700 "$voice_dir"
if [ ! -x "$voice_dir/bin/python" ]; then python3 -m venv "$voice_dir"; fi
"$voice_dir/bin/python" -m pip install 'faster-whisper==1.2.1' 'av==17.0.0' 'opencc-python-reimplemented==0.1.7'
"$voice_dir/bin/python" - "$voice_dir" "$project_dir" <<'PY'
import json,os,pathlib,sys
from faster_whisper.utils import download_model
root=pathlib.Path(sys.argv[1]).resolve();repo=pathlib.Path(sys.argv[2]).resolve()
model=download_model('base',output_dir=str(root/'models/base'))
path=root/'command.json'
fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
with os.fdopen(fd,'w') as out:json.dump([str(root/'bin/python'),str(repo/'scripts/transcribe_voice.py'),'--model',model,'--persistent'],out)
os.chmod(path,0o600)
print('Offline voice configuration ready:',path)
PY
