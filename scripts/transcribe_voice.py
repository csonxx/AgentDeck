#!/usr/bin/env python3
"""Offline ASR; persistent mode keeps weights in RAM, never saves audio."""
import argparse, io, json, struct, sys
from faster_whisper import WhisperModel
from opencc import OpenCC
p=argparse.ArgumentParser();p.add_argument('--model',required=True);p.add_argument('--persistent',action='store_true');p.add_argument('--language',default='zh');args=p.parse_args()
model=WhisperModel(args.model,device='cpu',compute_type='int8',local_files_only=True)
converter=OpenCC('t2s')
def transcribe(audio):
    if len(audio)>2<<20:raise ValueError('audio limit')
    segments,_=model.transcribe(io.BytesIO(audio),language=args.language,beam_size=1,vad_filter=True,condition_on_previous_text=False,chunk_length=10,temperature=0.0,without_timestamps=True,max_new_tokens=128)
    return converter.convert(''.join(s.text for s in segments).strip())
def exact(n):
    data=bytearray()
    while len(data)<n:
        chunk=sys.stdin.buffer.read(n-len(data))
        if not chunk:raise EOFError()
        data.extend(chunk)
    return bytes(data)
if args.persistent:
    while True:
        try:header=exact(4)
        except EOFError:break
        size=struct.unpack('>I',header)[0]
        if size<46 or size>2<<20:raise ValueError('audio frame limit')
        audio=exact(size)
        try:result={'text':transcribe(audio)}
        except Exception:result={'error':'transcription failed'}
        print(json.dumps(result,ensure_ascii=False),flush=True)
else:
    print(json.dumps({'text':transcribe(sys.stdin.buffer.read((2<<20)+1))},ensure_ascii=False))
