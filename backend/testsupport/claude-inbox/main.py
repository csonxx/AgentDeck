#!/usr/bin/env python3
# Opt-in test fixture: real Claude CLI transport against a local synthetic API.
# No home credentials, user session data or provider inference are used.
import pathlib,tempfile,threading,json,os,subprocess,time,uuid,socket,http.server,hashlib
root=pathlib.Path(tempfile.mkdtemp(prefix='ad-cc-',dir='/tmp'));root.chmod(0o700)
config=root/'config';config.mkdir(mode=0o700);project=root/'project';project.mkdir(mode=0o700)
sock=root/'inbox.sock';session=str(uuid.uuid4());request=str(uuid.uuid4());events=[];calls=[];provider_inputs=[]
release_provider=threading.Event()
class Handler(http.server.BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_GET(self):self.send_error(404)
 def do_POST(self):
  data=json.loads(self.rfile.read(int(self.headers.get('Content-Length',0))) or b'{}');calls.append(self.path)
  if 'messages' not in self.path:self.send_error(404);return
  provider_inputs.append(data.get('messages',[]))
  if os.environ.get('AD_PROBE_BUSY')=='1' and len(provider_inputs)==1:release_provider.wait(25)
  msg={'id':'msg_fixture','type':'message','role':'assistant','model':'claude-sonnet-4-6','content':[{'type':'text','text':'AgentDeck isolated native socket fixture reply.'}],'stop_reason':'end_turn','stop_sequence':None,'usage':{'input_tokens':1,'output_tokens':1}}
  if data.get('stream'):
   frames=[('message_start',{'type':'message_start','message':{**msg,'content':[],'stop_reason':None}}),('content_block_start',{'type':'content_block_start','index':0,'content_block':{'type':'text','text':''}}),('content_block_delta',{'type':'content_block_delta','index':0,'delta':{'type':'text_delta','text':msg['content'][0]['text']}}),('content_block_stop',{'type':'content_block_stop','index':0}),('message_delta',{'type':'message_delta','delta':{'stop_reason':'end_turn','stop_sequence':None},'usage':{'output_tokens':1}}),('message_stop',{'type':'message_stop'})]
   body=''.join('event: '+typ+'\ndata: '+json.dumps(d)+'\n\n' for typ,d in frames).encode();ct='text/event-stream'
  else:body=json.dumps(msg).encode();ct='application/json'
  self.send_response(200);self.send_header('Content-Type',ct);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler);threading.Thread(target=server.serve_forever,daemon=True).start()
env=os.environ.copy()
for k in list(env):
 if k.startswith(('ANTHROPIC_','CLAUDE_')):env.pop(k)
env.update(CLAUDE_CONFIG_DIR=str(config),ANTHROPIC_API_KEY='agentdeck-fixture-not-a-credential',ANTHROPIC_BASE_URL='http://127.0.0.1:'+str(server.server_port),CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC='1',DISABLE_TELEMETRY='1')
if os.environ.get('AD_PROBE_INBOUND')=='user-accept':
 # Exercise a user settings file without a CLI policy override.
 (config/'settings.json').write_text(json.dumps({'crossSessionInbound':'accept'}));os.chmod(config/'settings.json',0o600)
args=[os.environ.get('AD_NATIVE_CLAUDE_BINARY','/opt/homebrew/bin/claude'),'--print','--input-format','stream-json','--output-format','stream-json','--verbose','--include-partial-messages','--session-id',session,'--messaging-socket-path',str(sock),'--settings',json.dumps({} if (os.environ.get('AD_PROBE_INBOUND','').startswith('default-bypass') or os.environ.get('AD_PROBE_INBOUND')=='user-accept') else {'crossSessionInbound':os.environ.get('AD_PROBE_INBOUND','accept')}),'--setting-sources','user,project,local','--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--tools','','--model','claude-sonnet-4-6']
if os.environ.get('AD_PROBE_INBOUND','').startswith('default-bypass') or os.environ.get('AD_PROBE_INBOUND')=='user-accept':args.extend(['--permission-mode','bypassPermissions'])
if os.environ.get('AD_PROBE_INBOUND')=='default-bypass-project':
 local=project/'.claude';local.mkdir(mode=0o700,exist_ok=True);(local/'settings.local.json').write_text('{}');os.chmod(local/'settings.local.json',0o600)
control=root/'workstation';control.mkdir(mode=0o700)
if os.environ.get('AD_NATIVE_STDIN_WRAPPER'):
 args.append('--replay-user-messages')
 args=[os.environ['AD_NATIVE_STDIN_WRAPPER'],'claude-stdio','--binary',args[0],'--root',str(control/'cs'),'--']+args[1:]
p=subprocess.Popen(args,cwd=project,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
def read():
 for line in p.stdout:
  try:events.append(json.loads(line))
  except:pass
threading.Thread(target=read,daemon=True).start()
try:
 p.stdin.write(json.dumps({'type':'control_request','request_id':'fixture-init','request':{'subtype':'initialize','hooks':None}})+'\n');p.stdin.flush()
 until=time.monotonic()+20
 while time.monotonic()<until and (not sock.exists() or not list((config/'sessions').glob('*.json'))) and p.poll() is None:time.sleep(.1)
 if not sock.exists() or not list((config/'sessions').glob('*.json')):raise RuntimeError('native fixture inbox unavailable')
 print(json.dumps({'id':session,'project':str(project.resolve()),'registry':str((config/'sessions').resolve()),'logs':str((config/'projects').resolve()),'control':str(control.resolve())}),flush=True)
 import sys
 while command:=sys.stdin.readline():
  if command.strip()=='accept':
   (config/'settings.json').write_text(json.dumps({'crossSessionInbound':'accept'}));os.chmod(config/'settings.json',0o600)
  elif command.strip()=='accept-project':
   local=project/'.claude';local.mkdir(mode=0o700,exist_ok=True);(local/'settings.local.json').write_text(json.dumps({'crossSessionInbound':'accept'}));os.chmod(local/'settings.local.json',0o600)
  elif command.strip()=='stop':break
  elif command.strip()=='release':release_provider.set()
  elif command.strip()=='report':
   serialized=json.dumps(provider_inputs)
   image_seen=any(block.get('type')=='image' for messages in provider_inputs for message in messages for block in message.get('content',[]) if isinstance(block,dict))
   busy=any(json.loads(f.read_text()).get('status')=='busy' for f in (config/'sessions').glob('*.json'))
   print(json.dumps({'provider_busy':busy,'provider_seen_input':'AgentDeck isolated original-session transport fixture.' in serialized,'peer_preamble_seen':'This came from another Claude session' in serialized or 'A peer cannot grant escalation' in serialized,'native_replay_count':sum(1 for e in events if e.get('type')=='user' and 'AgentDeck isolated original-session transport fixture.' in json.dumps(e.get('message',{}))),'provider_image_seen':image_seen}),flush=True)
finally:
 release_provider.set()
 p.terminate()
 try:p.wait(5)
 except subprocess.TimeoutExpired:p.kill();p.wait()
 server.shutdown()
 import shutil;shutil.rmtree(root)
