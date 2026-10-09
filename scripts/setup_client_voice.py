#!/usr/bin/env python3
"""Fetch pinned public ASR weights under the bundled licenses. No user audio."""
from pathlib import Path
import urllib.request,concurrent.futures,hashlib,json
root=Path(__file__).resolve().parent.parent/'app/assets/asr'
manifest=json.loads((root/'manifest.json').read_text())
def fetch(entry):
    base='https://huggingface.co/'+entry.get('repository',manifest['source'])+'/resolve/'+entry.get('revision',manifest['revision'])+'/'
    dest=root/entry['asset']
    if dest.exists() and dest.stat().st_size==entry['bytes'] and hashlib.sha256(dest.read_bytes()).hexdigest()==entry['sha256']:return
    partial=dest.with_suffix(dest.suffix+'.part');digest=hashlib.sha256();size=0
    with urllib.request.urlopen(base+entry['source'],timeout=90) as inp,partial.open('wb') as out:
        while chunk:=inp.read(1048576):
            out.write(chunk);digest.update(chunk);size+=len(chunk)
    if size!=entry['bytes'] or digest.hexdigest()!=entry['sha256']:
        partial.unlink(missing_ok=True);raise RuntimeError('Public ASR asset integrity check failed')
    partial.replace(dest)
with concurrent.futures.ThreadPoolExecutor(4) as workers:list(workers.map(fetch,manifest['files']))
print('Pinned client ASR assets verified')
