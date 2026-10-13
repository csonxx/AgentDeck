#!/usr/bin/env python3
"""Install the macOS Claude Desktop native-stdin adapter, without app patching."""
import argparse
import os
from pathlib import Path
import plistlib
import shlex
import subprocess
import sys


def command(*argv, required=True):
    result = subprocess.run(argv, capture_output=True, text=True)
    if required and result.returncode:
        raise RuntimeError(f"{argv[0]} operation failed (exit {result.returncode})")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--agentdeck', type=Path)
    parser.add_argument('--native-binary', type=Path)
    parser.add_argument('--data', type=Path, default=Path.home()/'.agentdeck')
    parser.add_argument('--uninstall', action='store_true')
    parser.add_argument('--experimental', action='store_true', help='Isolated transport testing only; unsupported in production')
    args = parser.parse_args()
    if sys.platform != 'darwin':
        parser.error('Desktop activation currently supports macOS')
    os.umask(0o077)
    label = 'dev.agentdeck.claude-stdio'
    domain = f'gui/{os.getuid()}'
    tool = Path.home()/'.agentdeck-tools'/'claude-stdio'
    launcher = tool/'claude'
    plist = Path.home()/'Library'/'LaunchAgents'/f'{label}.plist'
    state = args.data.expanduser().resolve()/'cs'
    if args.uninstall:
        if command('/bin/launchctl','getenv','CLAUDE_CODE_LOCAL_BINARY',required=False).stdout.strip() == str(launcher):
            command('/bin/launchctl','unsetenv','CLAUDE_CODE_LOCAL_BINARY')
        command('/bin/launchctl','bootout',f'{domain}/{label}',required=False)
        plist.unlink(missing_ok=True)
        (state/'enabled').unlink(missing_ok=True)
        print('Claude stdin activation removed. Restart Claude Desktop to use its default executable.')
        return
    if not args.experimental:
        parser.error('Claude Desktop activation is unsupported. Use the managed Claude CLI through the workstation page; --uninstall removes the old setup.')
    if not args.agentdeck or not args.native_binary:
        parser.error('--agentdeck and --native-binary are required')
    binary, native = args.agentdeck.resolve(), args.native_binary.resolve()
    for p in (binary,native):
        if not p.is_file() or not os.access(p,os.X_OK):
            parser.error(f'executable unavailable: {p}')
    # The app's existing native executable is delegated to, never replaced.
    native_version=command(str(native),'--version').stdout.strip()
    wrapper_version=command(str(binary),'claude-stdio','--binary',str(native),'--root',str(state),'--','--version').stdout.strip()
    if native_version!=wrapper_version:
        raise RuntimeError('native version passthrough mismatch')
    tool.mkdir(mode=0o700,parents=True,exist_ok=True)
    state.mkdir(mode=0o700,parents=True,exist_ok=True)
    for p in (tool,state):
        if p.is_symlink() or p.stat().st_uid!=os.getuid() or p.stat().st_mode & 0o077:
            raise RuntimeError('adapter directories must be private and owned by the current user')
    argv=[str(binary),'claude-stdio','--binary',str(native),'--root',str(state),'--']
    content='#!/bin/sh\nexec '+' '.join(shlex.quote(v) for v in argv)+' "$@"\n'
    temporary=tool/'claude.next'
    temporary.write_text(content);temporary.chmod(0o700);temporary.replace(launcher)
    plist.parent.mkdir(parents=True,exist_ok=True)
    definition={'Label':label,'ProgramArguments':['/bin/launchctl','setenv','CLAUDE_CODE_LOCAL_BINARY',str(launcher)],'RunAtLoad':True}
    temporary=plist.with_suffix('.next')
    temporary.write_bytes(plistlib.dumps(definition));temporary.chmod(0o600);temporary.replace(plist)
    command('/bin/launchctl','bootout',f'{domain}/{label}',required=False)
    command('/bin/launchctl','bootstrap',domain,str(plist))
    command('/bin/launchctl','setenv','CLAUDE_CODE_LOCAL_BINARY',str(launcher))
    (state/'enabled').write_text('native-stdin-v1\n');(state/'enabled').chmod(0o600)
    print('Installed native stdin adapter. Running tasks are unchanged; restart Claude Desktop after they finish.')
    print(f'Launcher: {launcher}')


if __name__=='__main__':
    main()
