#!/usr/bin/env python3
"""Accuracy regression on generated speech; never records or reads user audio."""
import argparse,json,pathlib,shutil,subprocess,tempfile,wave,os
root=pathlib.Path(__file__).resolve().parent.parent
parser=argparse.ArgumentParser();parser.add_argument('--dart',default=shutil.which('dart'));parser.add_argument('--out');args=parser.parse_args()
if not args.dart or not shutil.which('say'):raise SystemExit('This synthetic suite needs Dart and macOS say; other platforms can supply fixtures to app/tool/check_client_voice.dart.')
phrases=[('draft','请帮我检查这个项目的代码，修复登录失败的问题。','Tingting',180),('ui','把输入框的字体缩小一点，按钮放到右边。','Tingting',205),('send','先运行测试，再把结果告诉我，不要自动发送消息。','Meijia',200),('stream','我希望长按说话的时候就能看到文字，松开以后还可以编辑。','Tingting',240),('mixed','使用 Flutter 开发客户端，后端使用 Go，连接 WebSocket。','Tingting',190),('model','在 Codex 会话里切换模型，Claude Code 也需要支持。','Tingting',190)]
with tempfile.TemporaryDirectory(prefix='agentdeck-voice-accuracy-') as name:
    base=pathlib.Path(name);fixtures=[];joined=[]
    for id,text,voice,rate in phrases:
        p=base/(id+'.wav');subprocess.run(['say','-v',voice,'-r',str(rate),'-o',str(p),'--file-format=WAVE','--data-format=LEI16@16000',text],check=True)
        fixtures.append({'id':id,'text':text,'path':str(p),'kind':'synthetic'})
        with wave.open(str(p)) as w:joined.extend([w.readframes(w.getnframes()),bytes(2*32000)])
    for id,text,pcm in [('silence','',bytes(5*32000)),('paused-long',''.join(x[1] for x in phrases),b''.join(joined))]:
        p=base/(id+'.wav')
        with wave.open(str(p),'wb') as w:w.setparams((1,2,16000,0,'NONE','not compressed'));w.writeframes(pcm)
        fixtures.append({'id':id,'text':text,'path':str(p),'kind':'silence' if id=='silence' else 'synthetic_with_pauses'})
    p=base/'fixtures.json';p.write_text(json.dumps(fixtures,ensure_ascii=False))
    run=subprocess.run([args.dart,'--packages=app/.dart_tool/package_config.json','app/tool/check_client_voice.dart',str(p)],cwd=root,capture_output=True,text=True)
    if run.returncode:raise SystemExit('Native ASR check failed; no owner audio was accessed. '+run.stderr[-1200:])
    rows=[json.loads(line) for line in run.stdout.splitlines() if line.startswith('{')]
    named={x['id']:x for x in rows if 'id' in x};short=[named[x[0]] for x in phrases]
    general=sum(named[x[0]]['errors'] for x in phrases[:4]);mixed=sum(named[x[0]]['errors'] for x in phrases[4:])
    passed=general<=5 and mixed<=14 and named['paused-long']['errors']<=14 and named['silence']['empty'] and rows[-1]['cancel_isolated'] and rows[-1]['recording_bounded']
    receipt={'synthetic_only':True,'microphone_opened':False,'audio_uploaded':False,'prompt_sent':False,'reference_characters':sum(x['reference_characters'] for x in short),'errors':sum(x['errors'] for x in short),'general_errors':general,'mixed_errors':mixed,'measurements':rows,'passed':passed}
    if args.out:pathlib.Path(args.out).write_text(json.dumps(receipt,indent=2)+'\n')
    print(json.dumps(receipt));raise SystemExit(0 if passed else 1)
