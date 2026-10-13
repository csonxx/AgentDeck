#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
run_dir="$(mktemp -d /tmp/agentdeck-verify.XXXXXX)"
backend_pid=""
proxy_backend_pid=""
proxy_pid=""
gateway_pid=""
relay_local_pid=""
cleanup() {
  for process_pid in "$relay_local_pid" "$gateway_pid" "$proxy_pid" "$proxy_backend_pid"; do
    if [ -n "$process_pid" ]; then kill "$process_pid" 2>/dev/null || true; wait "$process_pid" 2>/dev/null || true; fi
  done
  if [ -n "$backend_pid" ]; then kill "$backend_pid" 2>/dev/null || true; wait "$backend_pid" 2>/dev/null || true; fi
  rm -rf "$run_dir"
}
trap cleanup EXIT
mkdir -p "$run_dir/source" "$run_dir/bin" "$project_dir/artifacts"
python3 "$project_dir/scripts/setup_client_voice.py"
cp "$project_dir/scripts/fixture_runtime.py" "$run_dir/bin/codex"
cp "$project_dir/scripts/fixture_runtime.py" "$run_dir/bin/claude"
export PATH="$run_dir/bin:$PATH"
# Bulk Flutter integration uses a thirty-second rotation, still much shorter
# than the fifteen-minute production default. A very short key lifetime plus
# pure-Dart debug decryption of large fixtures can legitimately expire frames.
# Go's clock/replay/relay tests retain their one-second stress configuration.
export AGENTDECK_E2E_ROTATION_MS=30000
python3 - "$run_dir" <<'VOICEFIXTURE'
import json,os,pathlib,sys
p=pathlib.Path(sys.argv[1])/'voice-command.json'
fd=os.open(p,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
with os.fdopen(fd,'w') as out:json.dump([sys.executable,'-c',"import sys,json; audio=sys.stdin.buffer.read(); print(json.dumps({'text':'VOICE_FIXTURE_OK'}))"],out)
VOICEFIXTURE
cd "$project_dir/backend"
# Keep package processes sequential: gateway fixtures deliberately rotate every
# second while transferring multi-megabyte frames under race instrumentation.
# Contending package processes can starve that real protocol deadline on a busy
# workstation. This preserves race coverage, rotation checks and all timeouts.
go test -race -p 1 ./...
go vet ./...
# Replace atomically; overwriting a running signed executable can kill it on macOS.
go build -o "$project_dir/artifacts/agentdeck.next" ./cmd/agentdeck
mv "$project_dir/artifacts/agentdeck.next" "$project_dir/artifacts/agentdeck"
# Only sanitized synthetic inputs; no home-directory session logs are imported.
python3 - "$run_dir" <<'PY'
import json,sys,pathlib
r=pathlib.Path(sys.argv[1])
records=[{'type':'session_meta','payload':{'id':'e2e-session','cwd':str(r/'source')}}, {'method':'item/agentMessage/delta','params':{'threadId':'e2e-session','itemId':'m','delta':'Go → Flutter 实时会话\n[文件](linked.md:2)\n![图片](local.png)\n[视频](clip.mp4)\n[音频](tone.wav)\n[旧 Word](word97.doc)\n[旧 Excel](book97.xls)\n[文本](notes.txt)\n[大附件](large.bin)'}},
 {'type':'turn_context','payload':{'model':'fixture-native-model','effort':'high','service_tier':'priority'}},
 {'method':'item/reasoning/summaryTextDelta','params':{'threadId':'e2e-session','itemId':'think','summaryIndex':0,'delta':'检查加密连接与事件顺序'}},
 {'type':'response_item','payload':{'type':'message','role':'assistant','id':'image','content':[{'type':'input_image','image_url':'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg=='}]}}]
(r/'source/clip.mp4').write_bytes(bytes.fromhex('000000186674797069736f6d')+b'encrypted-video-fixture')
(r/'source/large.bin').write_bytes(bytes(range(256))*(36<<10))
(r/'source/linked.md').write_text('first\nEncrypted linked text\nlast\n')
import shutil, wave, struct
fixture_root=pathlib.Path.cwd()/'internal/document/testdata'
for name in ['word97.doc','book97.xls']:shutil.copyfile(fixture_root/name,r/'source'/name)
(r/'source/notes.txt').write_bytes('中文 text preview'.encode('utf-16'))
with wave.open(str(r/'source/tone.wav'),'wb') as audio:
 audio.setparams((1,2,16000,1600,'NONE','not compressed'));audio.writeframes(struct.pack('<h',0)*1600)

(r/'source/local.png').write_bytes(__import__('base64').b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg=='))
(r/'source/live.jsonl').write_text(''.join(json.dumps(v,ensure_ascii=False)+'\n' for v in records))
paged=[{'type':'session_meta','payload':{'id':'paged-session','cwd':str(r/'source')}}]
paged += [{'type':'response_item','payload':{'type':'message','role':'assistant','id':f'page-{i}','content':[{'type':'output_text','text':f'Paged message {i}'}]}} for i in range(240)]
(r/'source/paged.jsonl').write_text(''.join(json.dumps(v)+'\n' for v in paged))

PY
# Ask the OS for a free loopback port. A collision is reported as a failed startup.
test_port="$(python3 - <<'PY'
import socket
s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()
PY
)"
"$project_dir/artifacts/agentdeck" serve --no-console --voice-command-file "$run_dir/voice-command.json" --project-roots "$run_dir/source" --data "$run_dir/state" --listen "127.0.0.1:$test_port" --public-url "https://127.0.0.1:$test_port" --rotation 30s --codex-sessions "$run_dir/source" --media-roots "$run_dir/source" > "$run_dir/backend.log" 2>&1 &
backend_pid=$!
for attempt in $(seq 1 100); do
  if [ -S "$run_dir/state/control.sock" ]; then break; fi
  if ! kill -0 "$backend_pid" 2>/dev/null; then cat "$run_dir/backend.log"; exit 1; fi
  sleep 0.1
done
for fixture_agent in codex claude; do
  "$project_dir/artifacts/agentdeck" bridge --data "$run_dir/state" --agent "$fixture_agent" -- python3 "$project_dir/scripts/fixture_agent.py" "$fixture_agent" > "$run_dir/$fixture_agent.stdout"
  python3 - "$run_dir/$fixture_agent.stdout" "$fixture_agent" <<'PYTEST'
import json,sys
lines=[json.loads(x) for x in open(sys.argv[1])]
assert len(lines)==4, 'Bridge did not preserve stdout records'
PYTEST
done
# Exercise the actual CLI HTTP listener behind a separate TLS proxy. Certificates
# are held only by the fixture proxy, while the pairing URL remains HTTPS.
mkdir -p "$run_dir/proxy/source"
python3 - "$run_dir" "proxy" <<'PYCOPY'
import pathlib,sys,json,shutil
r=pathlib.Path(sys.argv[1]);dest=r/sys.argv[2]/'source'
rows=[json.loads(x) for x in (r/'source/live.jsonl').read_text().splitlines()]
rows[0]['payload']['cwd']=str(dest)
(dest/'live.jsonl').write_text(''.join(json.dumps(x,ensure_ascii=False)+'\n' for x in rows))
paged=[json.loads(x) for x in (r/'source/paged.jsonl').read_text().splitlines()]
paged[0]['payload']['cwd']=str(dest)
(dest/'paged.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in paged))

for name in ['linked.md','local.png','clip.mp4','tone.wav','word97.doc','book97.xls','notes.txt','large.bin']:shutil.copyfile(r/'source'/name,dest/name)
PYCOPY
read -r upstream_port proxy_port < <(python3 - <<'PY'
import socket
a=socket.socket();b=socket.socket()
a.bind(('127.0.0.1',0));b.bind(('127.0.0.1',0))
print(a.getsockname()[1],b.getsockname()[1]);a.close();b.close()
PY
)
"$project_dir/artifacts/agentdeck" serve --no-console --voice-command-file "$run_dir/voice-command.json" --project-roots "$run_dir/proxy/source" --behind-proxy --data "$run_dir/proxy/state" --listen "127.0.0.1:$upstream_port" --public-url "https://localhost:$proxy_port" --rotation 30s --codex-sessions "$run_dir/proxy/source" --media-roots "$run_dir/proxy/source" > "$run_dir/proxy/backend.log" 2>&1 &
proxy_backend_pid=$!
go build -o "$run_dir/tlsproxy" ./testsupport/proxy
"$run_dir/tlsproxy" --dir "$run_dir/proxy" --listen "127.0.0.1:$proxy_port" --upstream "http://127.0.0.1:$upstream_port" > "$run_dir/proxy/proxy.log" 2>&1 &
proxy_pid=$!
for attempt in $(seq 1 100); do
  if [ -S "$run_dir/proxy/state/control.sock" ] && [ -f "$run_dir/proxy/pin-after" ]; then break; fi
  if ! kill -0 "$proxy_backend_pid" 2>/dev/null || ! kill -0 "$proxy_pid" 2>/dev/null; then cat "$run_dir/proxy/backend.log" "$run_dir/proxy/proxy.log"; exit 1; fi
  sleep 0.1
done
if [ -f "$run_dir/proxy/state/tls.key" ]; then echo 'Proxy backend unexpectedly generated TLS credentials'; exit 1; fi
for fixture_agent in codex claude; do
  "$project_dir/artifacts/agentdeck" bridge --data "$run_dir/proxy/state" --agent "$fixture_agent" -- python3 "$project_dir/scripts/fixture_agent.py" "$fixture_agent" > "$run_dir/proxy/$fixture_agent.stdout"
done
# Three-role relay: synthetic Feishu PKCE callback, actual outbound Local CLI,
# and Flutter. No fixture identity/approval routes are in production binaries.
mkdir -p "$run_dir/gateway/source"
python3 - "$run_dir" "gateway" <<'PYCOPY'
import pathlib,sys,json,shutil
r=pathlib.Path(sys.argv[1]);dest=r/sys.argv[2]/'source'
rows=[json.loads(x) for x in (r/'source/live.jsonl').read_text().splitlines()]
rows[0]['payload']['cwd']=str(dest)
(dest/'live.jsonl').write_text(''.join(json.dumps(x,ensure_ascii=False)+'\n' for x in rows))
paged=[json.loads(x) for x in (r/'source/paged.jsonl').read_text().splitlines()]
paged[0]['payload']['cwd']=str(dest)
(dest/'paged.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in paged))

for name in ['linked.md','local.png','clip.mp4','tone.wav','word97.doc','book97.xls','notes.txt','large.bin']:shutil.copyfile(r/'source'/name,dest/name)
PYCOPY
gateway_port="$(python3 - <<'PYPORT'
import socket
s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()
PYPORT
)"
go build -o "$run_dir/gateway-fixture" ./testsupport/gateway
"$run_dir/gateway-fixture" --dir "$run_dir/gateway" --listen "127.0.0.1:$gateway_port" > "$run_dir/gateway/fixture.log" 2>&1 &
gateway_pid=$!
for attempt in $(seq 1 200); do
  if [ -f "$run_dir/gateway/metadata.json" ]; then break; fi
  if ! kill -0 "$gateway_pid" 2>/dev/null; then cat "$run_dir/gateway/fixture.log"; exit 1; fi
  sleep 0.1
done
"$project_dir/artifacts/agentdeck" serve --no-console --voice-command-file "$run_dir/voice-command.json" --project-roots "$run_dir/gateway/source" --relay --data "$run_dir/gateway/local-state" --rotation 30s --codex-sessions "$run_dir/gateway/source" --media-roots "$run_dir/gateway/source" > "$run_dir/gateway/local.log" 2>&1 &
relay_local_pid=$!
for attempt in $(seq 1 100); do
  if [ -S "$run_dir/gateway/local-state/control.sock" ] && rg -q "S-LOCAL linked" "$run_dir/gateway/local.log"; then break; fi
  if ! kill -0 "$relay_local_pid" 2>/dev/null; then cat "$run_dir/gateway/local.log"; exit 1; fi
  sleep 0.1
done
for fixture_agent in codex claude; do
  "$project_dir/artifacts/agentdeck" bridge --data "$run_dir/gateway/local-state" --agent "$fixture_agent" -- python3 "$project_dir/scripts/fixture_agent.py" "$fixture_agent" > "$run_dir/gateway/$fixture_agent.stdout"
done
cd "$project_dir/app"
flutter analyze
# Issue short-lived invites immediately before their tests. Running these first
# prevents a busy host / unrelated UI tests from expiring the production TTL.
"$project_dir/artifacts/agentdeck" pair --data "$run_dir/state" --out "$run_dir/pair"
"$project_dir/artifacts/agentdeck" pair --data "$run_dir/proxy/state" --out "$run_dir/proxy/pair"
AGENTDECK_E2E_DIR="$run_dir" flutter test test/e2e_test.dart --reporter expanded
# Keep live relay/reconnect integration separate from the concurrent widget
# suite, just like direct HTTPS above. Its Local fixture rotates keys every thirty seconds.
AGENTDECK_E2E_DIR="$run_dir" flutter test test/gateway_e2e_test.dart --reporter expanded
remaining_tests=()
for test_file in test/*_test.dart; do
  if [ "$test_file" != "test/e2e_test.dart" ] && [ "$test_file" != "test/gateway_e2e_test.dart" ]; then remaining_tests+=("$test_file"); fi
done
AGENTDECK_E2E_DIR="$run_dir" AGENTDECK_SCREENSHOTS=1 flutter test "${remaining_tests[@]}" --reporter expanded
printf 'Go/Flutter verification passed. Screenshots: %s/artifacts\n' "$project_dir"
