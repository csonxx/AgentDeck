#!/usr/bin/env python3
"""Synthetic ACP peer for protocol tests, never a production connector."""
import json, sys
if '--child-pid' in sys.argv:
 import subprocess,pathlib
 worker=subprocess.Popen([sys.executable,'-c','import time;time.sleep(300)'])
 pathlib.Path(sys.argv[sys.argv.index('--child-pid')+1]).write_text(str(worker.pid))
session='native-fixture'
modes={'currentModeId':'ask','availableModes':[{'id':'ask','name':'Ask'},{'id':'auto','name':'Auto'}]}
permission_test='--permissions' in sys.argv
pending=None
waiting={}
inputs='--inputs' in sys.argv
image_input=inputs and '--images-off' not in sys.argv
configs=[{'id':'mode','category':'mode','name':'Mode','type':'select','currentValue':'ask','options':[{'value':'ask','name':'Ask'},{'value':'auto','name':'Auto'}]}, {'id':'model','category':'model','name':'Model','type':'select','currentValue':'model-a','options':[{'group':'fixture','name':'Fixture','options':[{'value':'model-a','name':'Model A'},{'value':'model-b','name':'Model B'}]}]}, {'id':'reasoning','category':'thought_level','name':'Reasoning','type':'select','currentValue':'low','options':[{'value':'low','name':'Low'},{'value':'high','name':'High'}]}]
def send(v):
 print(json.dumps(dict(jsonrpc='2.0', **v)),flush=True)
def reply(v,result): send({'id':v['id'],'result':result})
def chunk(kind,text):
 send({'method':'session/update','params':{'sessionId':session,'update':{'sessionUpdate':kind,'content':{'type':'text','text':text}}}})
for line in sys.stdin:
 v=json.loads(line); p=v.get('params',{}); method=v.get('method')
 if method=='initialize':reply(v,{'protocolVersion':1,'authMethods':[],'agentCapabilities':{'loadSession':True,'sessionCapabilities':{'list':{}},'promptCapabilities':{'image':image_input}}})
 elif method=='session/new':
  assert set(p)<= {'cwd','mcpServers'}
  reply(v,dict(sessionId=session,**({'configOptions':configs} if inputs else {}),**({'modes':modes} if permission_test else {})))
 elif method=='session/load':
  assert set(p)<= {'sessionId','cwd','mcpServers'}
  if p['sessionId']!=session:send({'id':v['id'],'error':{'code':-32602,'message':'wrong session'}})
  else:chunk('agent_message_chunk','restored history');reply(v,{'configOptions':configs} if inputs else {})
 elif method=='session/set_mode':
  assert permission_test and p['sessionId']==session and p['modeId'] in ['ask','auto']
  modes['currentModeId']=p['modeId']
  send({'method':'session/update','params':{'sessionId':session,'update':{'sessionUpdate':'current_mode_update','currentModeId':p['modeId']}}})
  reply(v,{})
 elif method=='session/list':reply(v,{'sessions':[]})
 elif method=='session/set_config_option':
  assert inputs and p['sessionId']==session and p['configId']!='mode'
  option=next(x for x in configs if x['id']==p['configId'])
  option['currentValue']=p['value']
  if option['id']=='model':configs[2]['options']=[{'value':'low','name':'Low'}] if p['value']=='model-b' else [{'value':'low','name':'Low'},{'value':'high','name':'High'}]
  reply(v,{'configOptions':configs})
 elif method=='session/prompt':
  if p['sessionId']!=session:send({'id':v['id'],'error':{'code':-32602,'message':'wrong session'}});continue
  pending=v
  text=p['prompt'][0]['text']
  if text.startswith('input-check'):
   assert 'agentdeck-file:' in text and 'input-file' in text
   assert any(x['type']=='image' and x['mimeType']=='image/png' for x in p['prompt'])
   assert configs[0]['currentValue']=='ask' and configs[1]['currentValue']=='model-a' and configs[2]['currentValue']=='high'
   chunk('agent_message_chunk','native multimodal inputs received')
   reply(v,{'stopReason':'end_turn'});pending=None;continue
  if text=='approval':
   send({'id':'approval','method':'session/request_permission','params':{'sessionId':session,'toolCall':{'toolCallId':'tool1','title':'synthetic read'},'options':[{'optionId':'always','name':'Always','kind':'allow_always'},{'optionId':'once','name':'Once','kind':'allow_once'},{'optionId':'reject','name':'Reject','kind':'reject_once'}]}})
  elif text=='pause':chunk('agent_thought_chunk','waiting for cancel')
  else:
   chunk('agent_thought_chunk','reasoned ');chunk('agent_thought_chunk','step')
   send({'method':'session/update','params':{'sessionId':session,'update':{'sessionUpdate':'tool_call','toolCallId':'tool1','title':'Read fixture','status':'in_progress','rawInput':{'path':'fixture.md'}}}})
   send({'method':'session/update','params':{'sessionId':session,'update':{'sessionUpdate':'tool_call_update','toolCallId':'tool1','status':'completed','rawOutput':'read result'}}})
   send({'method':'session/update','params':{'sessionId':session,'update':{'sessionUpdate':'agent_message_chunk','content':{'type':'image','mimeType':'image/png','data':'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg=='}}}})
   chunk('agent_message_chunk','native ');chunk('agent_message_chunk','reply')
   reply(v,{'stopReason':'end_turn'});pending=None
 elif method=='session/cancel':
  if pending:reply(pending,{'stopReason':'cancelled'});pending=None
 elif v.get('id')=='approval' and 'result' in v:
  selected=v['result']['outcome'].get('optionId','cancelled');chunk('agent_message_chunk',selected)
  reply(pending,{'stopReason':'end_turn'});pending=None
