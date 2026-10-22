#!/usr/bin/env python3
"""Build signed S-LOCAL binaries and first-install bundles for six targets.

Private signing material is read only by the host publisher command. Neither
the key nor its path enters an installer, manifest, executable or log.
"""
import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import re
import shlex
import subprocess
import tarfile
import zipfile
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[1]


def source_digest():
    digest = hashlib.sha256()
    # Include embedded console/assets as well as Go files in release identity.
    paths = [path for path in (ROOT / 'backend').rglob('*')
             if path.is_file()
             and not any(part.startswith('.') for part in path.relative_to(ROOT / 'backend').parts)]
    for path in sorted(paths):
        digest.update(str(path.relative_to(ROOT)).encode() + b'\0')
        digest.update(path.read_bytes())
    return digest.hexdigest()


def https(value):
    u = urlparse(value)
    if u.scheme != 'https' or not u.netloc or u.username or u.query or u.fragment:
        raise argparse.ArgumentTypeError('Use an HTTPS URL without credentials or query')
    return value.rstrip('/')


def windows_cmd_arg(value):
    if any(c in value for c in '%!^&|<>\r\n"'):
        raise ValueError('Windows installer argument contains unsupported command characters')
    return '"' + value + '"'


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--version', required=True)
    p.add_argument('--build', type=int, required=True)
    p.add_argument('--gateway-url', type=https, required=True)
    p.add_argument('--download-base', type=https, required=True)
    p.add_argument('--publisher-key', required=True, help='key-id:base64-public-key')
    p.add_argument('--signing-key', type=Path, required=True)
    p.add_argument('--notes-file', type=Path)
    p.add_argument('--output', type=Path, default=ROOT / 'artifacts' / 'local')
    args = p.parse_args()
    if not re.fullmatch(r'\d+\.\d+\.\d+', args.version) or args.build < 1:
        p.error('version and monotonic positive build required')
    try:
        key_id, public_key = args.publisher_key.split(':', 1)
        assert re.fullmatch(r'[A-Za-z0-9_-]{1,64}', key_id)
        assert len(base64.b64decode(public_key, validate=True)) == 32
        private = base64.b64decode(args.signing_key.read_text().strip(), validate=True)
        assert len(private) == 64 and private[32:] == base64.b64decode(public_key)
    except (ValueError, AssertionError, OSError):
        p.error('Publisher public key and private signing file must match')
    del private
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    host = ('darwin' if platform.system() == 'Darwin' else platform.system().lower(),
            'arm64' if platform.machine().lower() in ('arm64', 'aarch64') else 'amd64')
    targets = [(system, arch) for system in ('darwin', 'windows', 'linux') for arch in ('amd64', 'arm64')]
    targets.sort(key=lambda target: target != host)
    rows = []
    publisher = None
    frozen_source = source_digest()
    for system, arch in targets:
        name = f'agentdeck-{system}-{arch}' + ('.exe' if system == 'windows' else '')
        binary = output / name
        env = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED='0')
        flags = f'-s -w -X agentdeck/internal/localupdate.Version={args.version} -X agentdeck/internal/localupdate.Build={args.build}'
        subprocess.run(['go', 'build', '-trimpath', '-ldflags', flags, '-o', str(binary), './cmd/agentdeck'], cwd=ROOT / 'backend', env=env, check=True)
        if (system, arch) == host:
            publisher = binary
        with binary.open('rb') as stream:
            digest = hashlib.file_digest(stream, 'sha256').hexdigest()
        rows.append(dict(os=system, arch=arch, file=name, sha256=digest,
                         bytes=binary.stat().st_size,
                         url=f'{args.download_base}/{digest}/{name}'))
        print(f'Built {system}/{arch}: {binary.stat().st_size} bytes', flush=True)
    assert publisher is not None
    if source_digest() != frozen_source:
        raise SystemExit('Backend sources changed during the build; no release manifests generated. Rebuild all targets.')
    for row in rows:
        system, arch = row['os'], row['arch']
        manifest = output / f'release-{system}-{arch}.json'
        command = [str(publisher), 'local-release-sign', '--key', str(args.signing_key.resolve()),
                   '--key-id', key_id, '--binary', str(output / row['file']), '--version', args.version,
                   '--build', str(args.build), '--os', system, '--arch', arch,
                   '--url', row['url'], '--out', str(manifest)]
        if args.notes_file:
            command += ['--notes-file', str(args.notes_file.resolve())]
        subprocess.run(command, check=True, stdout=subprocess.DEVNULL)
        row['manifest'] = manifest.name
        readme = ('AgentDeck S-LOCAL\n\n'
                  'macOS / Linux: run ./install.sh; Windows: double-click install.cmd, or run install.ps1 with PowerShell.\n'
                  '首次安装完成后，浏览器打开 http://127.0.0.1:8787/，关联飞书网关并确认手机配对。\n'
                  'After installation open http://127.0.0.1:8787/, link the gateway and pair your phone.\n'
                  '后续更新后台下载，校验发布签名，任务空闲后自动安装；启动失败会回退。\n'
                  'Later updates download in the background and install while idle, with signature checks and rollback.\n'
                  '已有手动运行的 Local 请先正常停止。不要删除 ~/.agentdeck，不用重新配对。\n'
                  'Stop a manually running Local before installing. Keep ~/.agentdeck and existing pairings.\n')
        files = {'README.txt': readme.encode(), 'agentdeck.exe' if system == 'windows' else 'agentdeck': (output / row['file']).read_bytes()}
        if system == 'windows':
            quote = lambda value: "'" + value.replace("'", "''") + "'"
            files['install.cmd'] = ('@echo off\r\n"%~dp0agentdeck.exe" local-install --gateway-url ' + windows_cmd_arg(args.gateway_url) + ' --publisher-key ' + windows_cmd_arg(args.publisher_key) + ' %*\r\nif errorlevel 1 (pause & exit /b 1)\r\necho Open http://127.0.0.1:8787/ to link your workstation.\r\npause\r\n').encode()
            files['install.ps1'] = ("$ErrorActionPreference = 'Stop'\n& (Join-Path $PSScriptRoot 'agentdeck.exe') local-install --gateway-url " + quote(args.gateway_url) + ' --publisher-key ' + quote(args.publisher_key) + " @args\nif ($LASTEXITCODE -ne 0) { throw 'AgentDeck installation failed' }\n").encode('utf-8-sig')
            bundle = output / f'agentdeck-local-{args.version}-{system}-{arch}.zip'
            with zipfile.ZipFile(bundle, 'w', zipfile.ZIP_DEFLATED) as archive:
                for name, data in files.items():
                    archive.writestr(name, data)
        else:
            files['install.sh'] = ('#!/bin/sh\nset -eu\nbase=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\nexec "$base/agentdeck" local-install --gateway-url ' + shlex.quote(args.gateway_url) + ' --publisher-key ' + shlex.quote(args.publisher_key) + ' "$@"\n').encode()
            bundle = output / f'agentdeck-local-{args.version}-{system}-{arch}.tar.gz'
            with tarfile.open(bundle, 'w:gz') as archive:
                for name, data in files.items():
                    info = tarfile.TarInfo(name); info.size = len(data)
                    info.mode = 0o700 if name in ('agentdeck', 'install.sh') else 0o600
                    archive.addfile(info, io.BytesIO(data))
        row['installer'] = bundle.name
        row['installer_bytes'] = bundle.stat().st_size
        with bundle.open('rb') as stream:
            row['installer_sha256'] = hashlib.file_digest(stream, 'sha256').hexdigest()
        row['installer_url'] = f"{args.download_base}/{row['installer_sha256']}/{bundle.name}"
    (output / 'index.json').write_text(json.dumps(dict(version=args.version, build=args.build, source_sha256=frozen_source, targets=rows), indent=2) + '\n')
    print('Six signed release manifests and six first-install bundles ready. Upload and verify every object before publishing manifests.')


if __name__ == '__main__':
    main()
