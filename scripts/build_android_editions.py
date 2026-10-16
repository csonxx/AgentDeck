#!/usr/bin/env python3
"""Build and verify voice/lite APKs from one frozen source tree."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import zipfile

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--edition', choices=['voice', 'lite', 'all'], default='all')
parser.add_argument('--flutter', default=os.environ.get('FLUTTER', str(Path.home()/'flutter/bin/flutter')))
args = parser.parse_args()
app = root/'app'
version = re.search(r'^version: (\d+\.\d+\.\d+)\+(\d+)$', (app/'pubspec.yaml').read_text(), re.M)
assert version
sdk = Path(os.environ.get('ANDROID_SDK_ROOT', str(Path.home()/'Library/Android/sdk')))
tools = sorted((sdk/'build-tools').glob('*/aapt'))
assert tools, 'Android build-tools required'
aapt = tools[-1]
apksigner = aapt.with_name('apksigner')
env = dict(os.environ)
env.setdefault('JAVA_HOME', '/Applications/Android Studio.app/Contents/jbr/Contents/Home')
env.setdefault('GRADLE_OPTS', '-Dorg.gradle.jvmargs=-Xmx2g -Dorg.gradle.workers.max=2')
env.setdefault('JAVA_TOOL_OPTIONS', '-XX:ActiveProcessorCount=2')

def freeze():
    files = list((app/'lib').rglob('*.dart')) + [app/'pubspec.yaml', app/'pubspec.lock']
    files += list((app/'android/app/src').rglob('*.kt')) + list((app/'android/app/src').rglob('*.xml'))
    files += [app/'android/app/build.gradle.kts', app/'android/settings.gradle.kts']
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(files)}

frozen = freeze()
editions = ['voice', 'lite'] if args.edition == 'all' else [args.edition]
results = []
for edition in editions:
    subprocess.run([args.flutter, 'build', 'apk', '--release', '--flavor', edition,
                    '--split-per-abi', '--target-platform', 'android-arm64'], cwd=app, env=env, check=True)
    assert freeze() == frozen, 'Sources changed during the release build'
    apk = app/f'build/app/outputs/flutter-apk/app-arm64-v8a-{edition}-release.apk'
    badging = subprocess.check_output([str(aapt), 'dump', 'badging', str(apk)], text=True)
    code = int(re.search(r"versionCode='(\d+)'", badging)[1])
    manifest_xml = subprocess.check_output([str(aapt), 'dump', 'xmltree', str(apk), 'AndroidManifest.xml'], text=True)
    flavor = re.search(r'android:name[^\n]*="dev\.agentdeck\.edition"[^\n]*\n[^\n]*android:value[^\n]*="(voice|lite)"', manifest_xml)
    assert flavor and flavor[1] == edition
    assert code == int(version[2]) + 2000
    assert f"versionName='{version[1]}'" in badging
    assert re.search(r"native-code: 'arm64-v8a'\s*$", badging, re.M)
    permissions = subprocess.check_output([str(aapt), 'dump', 'permissions', str(apk)], text=True)
    assert ('android.permission.RECORD_AUDIO' in permissions) == (edition == 'voice')
    cert = subprocess.check_output([str(apksigner), 'verify', '--print-certs', str(apk)], env=env, text=True)
    signer = re.search(r'certificate SHA-256 digest: ([a-f0-9]+)', cert)[1]
    with zipfile.ZipFile(apk) as z:
        names = z.namelist()
        models = [n for n in names if n.startswith('assets/flutter_assets/assets/asr/')]
        engine = [n for n in names if n.endswith('.so') and ('sherpa' in n or 'onnxruntime' in n)]
        assert bool(models) == bool(engine) == (edition == 'voice')
    target = root/f'artifacts/agentdeck-{version[1]}-arm64-v8a-{edition}.apk'
    target.parent.mkdir(exist_ok=True)
    shutil.copy2(apk, target)
    with target.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    row = dict(version=version[1], version_code=code, edition=edition, bytes=target.stat().st_size,
               sha256=digest, certificate_sha256=signer, apk=str(target),
               voice_model_files=len(models), voice_engine_libraries=len(engine), source_frozen=True)
    results.append(row)
    (root/f'artifacts/android-build-{version[1]}-{edition}.json').write_text(json.dumps(row, indent=2)+'\n')
    print(json.dumps(row), flush=True)
if len(results) == 2:
    assert results[0]['certificate_sha256'] == results[1]['certificate_sha256']
    assert results[1]['bytes'] < results[0]['bytes'] / 2
(root/f'artifacts/android-build-{version[1]}-source.json').write_text(json.dumps(frozen, indent=2)+'\n')
