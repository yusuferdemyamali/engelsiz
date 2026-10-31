#!/usr/bin/env python3
"""Compile the real installer definition with a temporary, non-release fixture."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', type=Path, required=True)
    parser.add_argument('--compiler', type=Path)
    args = parser.parse_args()
    if os.name != 'nt':
        parser.error('Run this compiler check on the Windows CI runner')
    app = args.app.resolve(strict=True)
    compiler = args.compiler or shutil.which('iscc')
    if compiler is None:
        compiler = Path(os.environ.get('ProgramFiles(x86)', 'C:/Program Files (x86)')) / 'Inno Setup 6/ISCC.exe'
    compiler = Path(compiler).resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix='engelsiz-installer-compile-') as temporary:
        stage = Path(temporary) / 'fixture'
        (stage / 'runtime').mkdir(parents=True)
        (stage / 'LICENSES').mkdir()
        shutil.copyfile(app, stage / 'engelsiz.exe')
        # This validates packaging syntax and file inclusion only. The copied
        # executable is deliberately not represented as a Kiwix distribution.
        shutil.copyfile(app, stage / 'runtime/kiwix-serve.exe')
        shutil.copyfile(ROOT / 'LICENSE', stage / 'LICENSES/Engelsiz-LICENSE')
        (stage / 'README.txt').write_text('Compiler fixture only; not a distributable Engelsiz package.\n')
        output = Path(temporary) / 'output'
        subprocess.run([
            str(compiler), f'/DStageDir={stage}', f'/DOutputDir={output}',
            '/DAppVersion=0.1.0', str(ROOT / 'packaging/windows/engelsiz.iss'),
        ], check=True)
        installer = output / 'Engelsiz-Setup.exe'
        if not installer.is_file() or installer.stat().st_size == 0:
            raise RuntimeError('Inno Setup did not produce the compiler fixture')
        print('Installer definition compiled; temporary fixture will be removed.')


if __name__ == '__main__':
    main()
