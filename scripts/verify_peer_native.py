#!/usr/bin/env python3
"""Verify native Flutter/WebRTC against an isolated Go Gateway and Local.

Uses a separate, disposable Android application ID. Real AgentDeck pairing,
agents, account credits and the running workstation are never touched.
"""
import argparse,base64,hashlib,json,os,pathlib,shutil,socket,subprocess,time,tempfile
ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--device',required=True,help='ADB serial of the authorized test phone')
p.add_argument('--flutter',default=str(pathlib.Path.home()/'flutter/bin/flutter'))
p.add_argument('--adb',default=str(pathlib.Path.home()/'Library/Android/sdk/platform-tools/adb'))
a=p.parse_args()
run=ROOT/'.run/peer-native';run.mkdir(parents=True,exist_ok=True,mode=0o700)
APP='dev.agentdeck.p2pacceptance'

def call(args,**kw):
 result=subprocess.run(args,**kw)
 if result.returncode:raise RuntimeError('Fixture command failed: '+pathlib.Path(args[0]).name+'; inspect its local log')
 return result
def adb(*args,**kw):return call([a.adb,'-s',a.device,*args],**kw)
def free_port():
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
def source_hash():
 files=list((ROOT/'backend').rglob('*.go'))+[ROOT/'backend/go.mod',ROOT/'backend/go.sum',ROOT/'app/pubspec.yaml',ROOT/'app/pubspec.lock']
 files+=list((ROOT/'app/lib').rglob('*.dart'))+list((ROOT/'app/test_support').rglob('*.dart'))
 files+=list((ROOT/'backend/internal/console/web').rglob('*'))
 digest=hashlib.sha256()
 for path in sorted(p for p in files if p.is_file()):digest.update(str(path.relative_to(ROOT)).encode()+b'\0'+path.read_bytes())
 return digest.hexdigest()
frozen=source_hash()
processes=[];reverse=False;installed=False;state=None
try:
 if subprocess.check_output([a.adb,'-s',a.device,'get-state'],text=True).strip()!='device':raise RuntimeError('Authorized Android device is not ready')
 app=run/'app';app.mkdir(exist_ok=True)
 for name in ['lib','test_support','android']:
  shutil.copytree(ROOT/'app'/name,app/name,dirs_exist_ok=True,ignore=shutil.ignore_patterns('build','.gradle','.cxx','local.properties','key.properties','*.jks','*.keystore'))
 for name in ['pubspec.yaml','pubspec.lock','.metadata']:
  if (ROOT/'app'/name).exists():shutil.copy2(ROOT/'app'/name,app/name)
 shutil.copytree(ROOT/'app/assets/brand',app/'assets/brand',dirs_exist_ok=True)
 (app/'assets/asr').mkdir(parents=True,exist_ok=True)
 gradle=app/'android/app/build.gradle.kts';text=gradle.read_text().replace('applicationId = "dev.agentdeck.agentdeck"','applicationId = "'+APP+'"');gradle.write_text(text)
 manifest=app/'android/app/src/main/AndroidManifest.xml';text=manifest.read_text().replace('android:label="AgentDeck"','android:label="AgentDeck P2P acceptance"')
 for cls in ['DeckApplication','MainActivity','DownloadService','OutboxService']:text=text.replace('android:name=".'+cls+'"','android:name="dev.agentdeck.agentdeck.'+cls+'"')
 manifest.write_text(text)
 for target,package in [('local','./cmd/agentdeck'),('gateway','./testsupport/gateway')]:
  call(['go','build','-o',str(run/target),package],cwd=ROOT/'backend')
 source=run/'source';source.mkdir(exist_ok=True)
 file_bytes=bytes(range(256))*4096;(source/'large.bin').write_bytes(file_bytes)
 image='iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg=='
 records=[{'type':'session_meta','payload':{'id':'peer-session','cwd':str(source)}},{'type':'response_item','payload':{'type':'message','role':'assistant','id':'text','content':[{'type':'output_text','text':'Synthetic native transport acceptance.\n[File](large.bin)'}]}},{'type':'response_item','payload':{'type':'message','role':'assistant','id':'image','content':[{'type':'input_image','image_url':'data:image/png;base64,'+image}]}}]
 (source/'fixture.jsonl').write_text(''.join(json.dumps(v)+'\n' for v in records))
 # Per-run state avoids cached checkpoints or a prior successful test result.
 state=pathlib.Path(tempfile.mkdtemp(prefix='ad-peer-',dir='/tmp'))
 port=free_port()
 def start(args,name,env=None):
  log=(run/(name+'.log')).open('w');process=subprocess.Popen(args,stdout=log,stderr=subprocess.STDOUT,env=env);processes.append((process,log));return process
 gateway=start([str(run/'gateway'),'--dir',str(state),'--listen',f'127.0.0.1:{port}'],'gateway')
 for _ in range(200):
  if (state/'metadata.json').exists():break
  if gateway.poll() is not None:raise RuntimeError('Synthetic gateway startup failed')
  time.sleep(.1)
 else:raise RuntimeError('Synthetic gateway startup timed out')
 # Only fixture agents can be resolved, even if the real workstation has agents.
 fixture_bin=run/'bin';fixture_bin.mkdir(exist_ok=True)
 for agent in ['codex','claude']:shutil.copy2(ROOT/'scripts/fixture_runtime.py',fixture_bin/agent);(fixture_bin/agent).chmod(0o700)
 env=dict(os.environ);env['PATH']=str(fixture_bin)+os.pathsep+env['PATH']
 local=start([str(run/'local'),'serve','--relay','--no-console','--data',str(state/'local-state'),'--rotation','30s','--codex-sessions',str(source),'--project-roots',str(source),'--media-roots',str(source)],'local',env)
 for _ in range(200):
  if (state/'local-state/control.sock').exists() and 'S-LOCAL linked' in (run/'local.log').read_text():break
  if local.poll() is not None:raise RuntimeError('Synthetic Local startup failed')
  time.sleep(.1)
 else:raise RuntimeError('Synthetic Local startup timed out')
 meta=json.loads((state/'metadata.json').read_text());meta['pem']=(state/'tls/tls.pem').read_text();meta['file_sha256']=hashlib.sha256(file_bytes).hexdigest()
 config=base64.b64encode(json.dumps(meta).encode()).decode()
 env=dict(os.environ);env.setdefault('JAVA_HOME','/Applications/Android Studio.app/Contents/jbr/Contents/Home');env.setdefault('JAVA_TOOL_OPTIONS','-XX:ActiveProcessorCount=2');env.setdefault('GRADLE_OPTS','-Dorg.gradle.jvmargs=-Xmx2g -Dorg.gradle.workers.max=2')
 # Fixture config is public TLS material and synthetic IDs only, never user keys.
 build=[a.flutter,'build','apk','--debug','--flavor','lite','--target-platform','android-arm64','--target','test_support/peer_native_main.dart','--dart-define=AD_PEER_FIXTURE='+config]
 print("Building isolated native acceptance APK",flush=True)
 with (run/'android-build.log').open('w') as log:call(build,cwd=app,env=env,stdout=log,stderr=subprocess.STDOUT)
 apk=app/'build/app/outputs/flutter-apk/app-lite-debug.apk'
 adb('reverse',f'tcp:{port}',f'tcp:{port}',capture_output=True);reverse=True
 adb('install','-r',str(apk),capture_output=True);installed=True
 adb('shell','input','keyevent','KEYCODE_WAKEUP',capture_output=True)
 adb('shell','wm','dismiss-keyguard',capture_output=True)
 adb('shell','am','start','-n',APP+'/dev.agentdeck.agentdeck.MainActivity',capture_output=True)
 for _ in range(50):
  pid=adb('shell','pidof',APP,capture_output=True,text=True).stdout.strip()
  if pid:break
  time.sleep(.1)
 if pid:
  log=(run/'native-app.log').open('w');process=subprocess.Popen([a.adb,'-s',a.device,'logcat','--pid='+pid,'-v','brief'],stdout=log,stderr=subprocess.STDOUT);processes.append((process,log))
 result=state/'native-result.json'
 for _ in range(1500):
  if result.exists():break
  if any(process.poll()is not None for process,_ in processes[:2]):raise RuntimeError('Fixture service exited during native acceptance')
  time.sleep(.1)
 else:raise RuntimeError('Native result not received; inspect the fixture app logs')
 assert source_hash()==frozen,'Sources changed during native acceptance; rerun the test'
 report=json.loads(result.read_text());report['source_sha256']=frozen;report['device']=a.device;report['fixture_app_id']=APP
 (run/'acceptance.json').write_text(json.dumps(report,indent=2)+'\n')
 print(json.dumps(report,indent=2),flush=True)
 if report.get('passed')is not True:raise RuntimeError('Native acceptance did not pass')
finally:
 if installed:adb('uninstall',APP,capture_output=True)
 if reverse:adb('reverse','--remove',f'tcp:{port}',capture_output=True)
 for process,log in reversed(processes):
  process.terminate()
  try:process.wait(timeout=15)
  except subprocess.TimeoutExpired:process.kill();process.wait()
  log.close()
 if state is not None:shutil.rmtree(state)
