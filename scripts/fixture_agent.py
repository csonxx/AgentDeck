#!/usr/bin/env python3
"""Sanitized stdout fixture for the actual IPC bridge (no model calls)."""
import json, sys, time
agent = sys.argv[1]
if agent == 'codex':
    records = [
        {'method':'thread/started','params':{'thread':{'id':'bridge-codex'}}},
        {'method':'item/agentMessage/delta','params':{'threadId':'bridge-codex','itemId':'bridge-m','delta':'bridge '}},
        {'method':'item/agentMessage/delta','params':{'threadId':'bridge-codex','itemId':'bridge-m','delta':'works'}},
        {'method':'item/completed','params':{'threadId':'bridge-codex','item':{'type':'agentMessage','id':'bridge-m','text':'bridge works'}}},
    ]
else:
    records = [
        {'type':'stream_event','session_id':'bridge-claude','event':{'type':'message_start','message':{'id':'bridge-cl'}}},
        {'type':'stream_event','session_id':'bridge-claude','event':{'type':'content_block_delta','index':0,'delta':{'type':'text_delta','text':'Claude bridge works'}}},
        {'type':'assistant','session_id':'bridge-claude','message':{'id':'bridge-cl','role':'assistant','content':[{'type':'text','text':'Claude bridge works'}]}},
        {'type':'result','session_id':'bridge-claude','is_error':False},
    ]
for record in records:
    print(json.dumps(record), flush=True)
    time.sleep(.05)
