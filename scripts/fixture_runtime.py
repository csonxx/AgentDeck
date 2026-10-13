#!/usr/bin/env python3
"""Protocol fixture for tests only. Never installed in production PATH."""
import sys,json,uuid,os,pathlib
claude=os.path.basename(sys.argv[0])=='claude'
session=next((x.split('=',1)[1] for x in sys.argv if x.startswith('--session-id=') or x.startswith('--resume=')),str(uuid.uuid4()))
strict=os.environ.get('AGENTDECK_FIXTURE_SESSION_DIR')
if claude and strict and '--no-session-persistence' not in sys.argv:
 marker=pathlib.Path(strict)/session
 resumed=any(x.startswith('--resume=') for x in sys.argv) and '--fork-session' not in sys.argv
 if resumed and not marker.exists():sys.exit(1)
 if not resumed and marker.exists():sys.exit(1)
turn='';pending=False;message=''
def send(v):print(json.dumps(v),flush=True)
def finish(text='RUNTIME_OK',status='completed'):
 global pending
 pending=False
 if claude:
  mid=str(uuid.uuid4());send({'type':'stream_event','session_id':session,'event':{'type':'message_start','message':{'id':mid}}})
  send({'type':'stream_event','session_id':session,'event':{'type':'content_block_delta','index':0,'delta':{'type':'text_delta','text':text}}})
  send({'type':'assistant','session_id':session,'message':{'id':mid,'role':'assistant','content':[{'type':'text','text':text}]}})
  send({'type':'result','session_id':session,'is_error':False})
 else:
  item=str(uuid.uuid4());send({'method':'item/agentMessage/delta','params':{'threadId':session,'itemId':item,'delta':text}})
  send({'method':'item/completed','params':{'threadId':session,'item':{'type':'agentMessage','id':item,'text':text}}})
  send({'method':'turn/completed','params':{'threadId':session,'turn':{'id':turn,'status':status}}})
for line in sys.stdin:
 d=json.loads(line)
 if claude:
  if d.get('type')=='control_request':
   if d['request']['subtype']=='get_usage':
    send({'type':'control_response','response':{'subtype':'success','request_id':d['request_id'],'response':{'rate_limits_available':True,'rate_limits':{'five_hour':{'utilization':25,'resets_at':'2030-01-01T00:00:00Z'}}}}});continue
   send({'type':'control_response','response':{'subtype':'success','request_id':d['request_id'],'response':{'models':[{'value':'fixture-model','displayName':'Fixture model','supportsEffort':True,'supportsFastMode':True,'supportedEffortLevels':['low','high']}]}}})
   if d['request']['subtype']=='interrupt':finish('STOPPED','interrupted')
   continue
  if d.get('type')=='control_response':
   finish('APPROVED' if d['response']['response']['behavior']=='allow' else 'DECLINED');continue
  if d.get('type')!='user':continue
  if strict:(pathlib.Path(strict)/session).write_text('fixture native history marker')
  message=d['message']['content']
  if isinstance(message,list):message='\n'.join(x.get('text','') for x in message if x.get('type')=='text')
 else:
  method=d.get('method');params=d.get('params',{})
  if method in ('thread/goal/get','thread/goal/set','thread/goal/clear'):
   path=os.environ.get('AGENTDECK_FIXTURE_GOALS');goals=json.loads(pathlib.Path(path).read_text()) if path and pathlib.Path(path).exists() else {}
   gid=params['threadId']
   if method=='thread/goal/set':
    assert params.get('origin')=='user'
    goals[gid]['status']=params['status'];goals[gid]['updatedAt']+=1
   if method=='thread/goal/clear':
    assert params.get('origin')=='user'
    goals.pop(gid,None)
   if path:pathlib.Path(path).write_text(json.dumps(goals))
   dbpath=os.environ.get('AGENTDECK_FIXTURE_GOAL_DB')
   if dbpath and method!='thread/goal/get':
    import sqlite3
    with sqlite3.connect(dbpath) as db:
     if method=='thread/goal/clear':db.execute('DELETE FROM thread_goals WHERE thread_id=?',(gid,))
     else:db.execute('UPDATE thread_goals SET status=? WHERE thread_id=?',(params['status'],gid))
   send({'id':d['id'],'result':{'cleared':True} if method=='thread/goal/clear' else {'goal':goals.get(gid)}});continue
  if method=='account/rateLimits/read':
   send({'id':d['id'],'result':{'rateLimits':{'primary':{'usedPercent':25,'windowDurationMins':300,'resetsAt':1893456000}}}});continue
  if method=='model/list':
   send({'id':d['id'],'result':{'data':[{'id':'fixture-model','model':'fixture-model','displayName':'Fixture model','defaultReasoningEffort':'low','supportedReasoningEfforts':[{'reasoningEffort':'low'},{'reasoningEffort':'high'}],'inputModalities':['text','image'],'serviceTiers':[{'id':'priority','name':'Fast'}],'isDefault':True}],'nextCursor':None}});continue
  if method in ('initialize','thread/start','thread/resume','thread/fork'):
   send({'id':d['id'],'result':{'thread':{'id':session}}});continue
  if method=='turn/interrupt':send({'id':d['id'],'result':{}});finish('STOPPED','interrupted');continue
  if 'result' in d:finish('APPROVED' if d['result'].get('decision')=='accept' else 'DECLINED');continue
  if method!='turn/start':continue
  turn=str(uuid.uuid4());message=params['input'][0]['text'];send({'id':d['id'],'result':{'turn':{'id':turn}}})
  send({'method':'item/completed','params':{'threadId':session,'item':{'type':'userMessage','id':str(uuid.uuid4()),'content':params['input']}}})
 if 'approval' in message.lower():
  pending=True
  if claude:send({'type':'control_request','request_id':'permission','request':{'subtype':'can_use_tool','tool_name':'Write','input':{'file_path':'fixture.txt','content':'fixture'}}})
  else:send({'id':42,'method':'item/commandExecution/requestApproval','params':{'threadId':session,'turnId':turn,'command':'echo fixture','reason':'Fixture approval'}})
 elif 'pause' in message.lower():pending=True
 else:
  config=('model='+next((x.split('=',1)[1] for x in sys.argv if x.startswith('--model=')),'inherit')+' effort='+next((x.split('=',1)[1] for x in sys.argv if x.startswith('--effort=')),'inherit')) if claude else ('model='+params.get('model','inherit')+' effort='+params.get('effort','inherit')+' speed='+params.get('serviceTier','inherit'))
  finish('RUNTIME_OK '+config if 'generation-check' in message else 'RUNTIME_OK')
