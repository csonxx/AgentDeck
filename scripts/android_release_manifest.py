#!/usr/bin/env python3
"""Build a public update manifest from a verified APK and infras receipt."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import urlparse


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apk', required=True, type=Path)
    parser.add_argument('--receipt', required=True, type=Path)
    parser.add_argument('--notes-file', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--aapt', type=Path)
    args = parser.parse_args()
    tools = sorted((Path(os.environ.get('ANDROID_SDK_ROOT', Path.home()/'Library/Android/sdk'))/'build-tools').glob('*/aapt'))
    aapt = args.aapt or (tools[-1] if tools else None)
    if aapt is None:
        parser.error('Provide --aapt from Android SDK build-tools')
    metadata = subprocess.check_output([str(aapt), 'dump', 'badging', str(args.apk)], text=True)
    package = re.search(r"package: name='([^']+)' versionCode='(\d+)' versionName='([^']+)'", metadata)
    sdk = re.search(r"sdkVersion:'(\d+)'", metadata)
    assert package and sdk and package[1] == 'dev.agentdeck.agentdeck'
    assert re.search(r"native-code: 'arm64-v8a'\s*$", metadata, re.M)
    with args.apk.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    receipt = json.loads(args.receipt.read_text())
    assert receipt['sha256'] == digest and receipt['bytes'] == args.apk.stat().st_size
    assert receipt.get('anonymous_https_full_download_verified') is True
    url = urlparse(receipt['url'])
    assert url.scheme == 'https' and url.hostname and not url.username and not url.query and not url.fragment
    notes = args.notes_file.read_text().strip()
    assert len(notes.encode()) <= 12000
    manifest = {'version': package[3], 'version_code': int(package[2]),
                'package': package[1], 'abi': 'arm64-v8a', 'min_sdk': int(sdk[1]),
                'bytes': args.apk.stat().st_size, 'sha256': digest,
                'url': receipt['url'], 'notes': notes}
    args.output.write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print(f'Android {manifest["version"]} / code {manifest["version_code"]}: manifest saved to {args.output}')


if __name__ == '__main__':
    main()
