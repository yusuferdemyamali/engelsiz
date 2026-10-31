#!/usr/bin/env python3
"""Maintainer-only release builder. Missing reviewed inputs are fatal, never skipped."""
from contextlib import ExitStack
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
APPLICATION_NOTICE_INPUTS = ('go.mod', 'go.sum', 'frontend/package-lock.json', 'frontend/tailwind.config.js')


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def checked(path, expected):
    if not isinstance(expected, str) or not re.fullmatch(r'[a-f0-9]{64}', expected):
        raise ValueError(f'Missing reviewed SHA-256 pin for {Path(path).name}')
    if not Path(path).is_file() or Path(path).is_symlink() or digest(path) != expected:
        raise ValueError(f'SHA-256 mismatch or unsafe input: {Path(path).name}')
    return Path(path)


def owned_file(root, name):
    if not isinstance(name, str) or not name or '\\' in name:
        raise ValueError('Invalid compliance filename')
    path = PurePosixPath(name)
    if path.is_absolute() or any(p in ('', '.', '..') for p in path.parts) or ':' in name:
        raise ValueError('Compliance path escapes bundle')
    result = Path(root).joinpath(*path.parts)
    for part in [result, *result.parents]:
        if part == Path(root):
            break
        if part.is_symlink():
            raise ValueError('Symlink in compliance bundle')
    if not result.is_file() or result.stat().st_size == 0 or not result.resolve().is_relative_to(Path(root).resolve()):
        raise ValueError(f'Missing compliance file: {name}')
    return result


def compliance(root, lock, project=ROOT):
    license_file = Path(project) / 'LICENSE'
    if not license_file.is_file() or license_file.is_symlink() or license_file.stat().st_size < 100:
        raise ValueError('Approved project LICENSE is missing; distribution blocked')
    manifest_file = checked(Path(root) / 'manifest.json', lock.get('complianceManifestSha256'))
    data = json.loads(manifest_file.read_text())
    if data.get('runtimeSha256') != lock['sha256'] or data.get('runtimeVersion') != lock['version']:
        raise ValueError('Compliance inventory does not match pinned runtime')
    if not data.get('reviewedBy') or not data.get('reviewedAt') or data.get('completeCorrespondingSource') is not True:
        raise ValueError('Complete corresponding source review is missing')
    components = data.get('components', [])
    if not components or 'kiwix-tools' not in [c.get('name') for c in components]:
        raise ValueError('Source inventory lacks kiwix-tools')
    for component in components:
        for field in ('name', 'version', 'license', 'licenseFile', 'licenseSha256', 'sourceArchive', 'sourceSha256', 'buildInstructions', 'buildInstructionsSha256'):
            if not component.get(field):
                raise ValueError(f'Incomplete component inventory: {field}')
        checked(owned_file(root, component['licenseFile']), component['licenseSha256'])
        checked(owned_file(root, component['buildInstructions']), component['buildInstructionsSha256'])
        checked(owned_file(root, component['sourceArchive']), component['sourceSha256'])
    application_inventory = application_notice_inventory(project)
    for scope in ('go', 'frontend'):
        notices = data.get('applicationDependencies', {}).get(scope, {})
        expected = application_inventory['outputs'][scope + '.txt']['sha256']
        if notices.get('noticeSha256') != expected:
            raise ValueError(f'Bundled {scope} attribution is stale for the current application notice inventory')
        checked(owned_file(root, notices.get('noticeFile')), notices.get('noticeSha256'))
    # Source bundle may contain extra files, but symlinks/special files are never copied.
    for path in Path(root).rglob('*'):
        if path.is_symlink() or (not path.is_file() and not path.is_dir()):
            raise ValueError('Unsafe member in compliance bundle')
    return data


def application_notice_inventory(project):
    """Bind bundled attribution to the app inputs being packaged now."""
    inventory_path = owned_file(project, 'third_party/application-notices/inventory.json')
    inventory = json.loads(inventory_path.read_text())
    if not isinstance(inventory, dict) or inventory.get('format') != 1 or not isinstance(inventory.get('inputs'), dict) or not isinstance(inventory.get('outputs'), dict):
        raise ValueError('Missing or malformed application notice inventory')
    for name in APPLICATION_NOTICE_INPUTS:
        try:
            checked(owned_file(project, name), inventory['inputs'].get(name))
        except ValueError as error:
            raise ValueError(f'Application notice inventory is stale: {name}; regenerate attribution material') from error
    for scope in ('go', 'frontend'):
        output = inventory['outputs'].get(scope + '.txt')
        if not isinstance(output, dict):
            raise ValueError(f'Application notice inventory lacks {scope} output')
        checked(owned_file(project, 'third_party/application-notices/' + scope + '.txt'), output.get('sha256'))
    return inventory


def safe_member(name, root):
    if '\\' in name or ':' in name or name.startswith('/'):
        raise ValueError('Unsafe runtime archive path')
    path = PurePosixPath(name)
    if '..' in path.parts or not path.parts or (root and path.parts[0] != root):
        raise ValueError('Runtime archive member escapes expected root')
    return path


def extract_runtime(archive, lock, destination):
    checked(archive, lock['sha256'])
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    executable = lock['executable']
    found = False
    seen = set()
    # Keep all upstream runtime files/DLLs; reject links/specials before copying anything.
    if str(archive).endswith('.zip'):
        with zipfile.ZipFile(archive) as bundle:
            entries = bundle.infolist()
            for member in entries:
                safe_member(member.filename, lock['root'])
                mode = member.external_attr >> 16
                if (mode & 0o170000) not in (0, 0o100000, 0o040000):
                    raise ValueError('Nonregular runtime archive member')
            for member in entries:
                path = safe_member(member.filename, lock['root'])
                if member.is_dir():
                    continue
                relative = Path(*(path.parts[1:] if lock['root'] else path.parts))
                if str(relative).lower() in seen:
                    raise ValueError('Duplicate runtime archive member')
                seen.add(str(relative).lower())
                target = destination / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                with bundle.open(member) as source, target.open('xb') as output:
                    shutil.copyfileobj(source, output)
                found |= relative == Path(executable)
    else:
        with tarfile.open(archive, 'r:*') as bundle:
            entries = bundle.getmembers()
            for member in entries:
                safe_member(member.name, lock['root'])
                if not member.isfile() and not member.isdir():
                    raise ValueError('Nonregular runtime archive member')
            for member in entries:
                path = safe_member(member.name, lock['root'])
                if member.isdir():
                    continue
                relative = Path(*(path.parts[1:] if lock['root'] else path.parts))
                if str(relative).lower() in seen:
                    raise ValueError('Duplicate runtime archive member')
                seen.add(str(relative).lower())
                target = destination / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                with bundle.extractfile(member) as source, target.open('xb') as output:
                    shutil.copyfileobj(source, output)
                target.chmod(0o755 if relative.name.startswith('kiwix-') else 0o644)
                found |= relative == Path(executable)
    if not found:
        raise ValueError('Pinned archive lacks expected runtime executable')
    return destination / executable


def static_elf(path):
    body = Path(path).read_bytes()
    if body[:6] != b'\x7fELF\x02\x01' or len(body) < 64 or struct.unpack_from('<H', body, 18)[0] != 62:
        raise ValueError('Expected Linux x86_64 ELF')
    offset = struct.unpack_from('<Q', body, 32)[0]
    size, count = struct.unpack_from('<HH', body, 54)
    if size < 56 or offset + size * count > len(body):
        raise ValueError('Invalid ELF program headers')
    for i in range(count):
        if struct.unpack_from('<I', body, offset + i * size)[0] in (2, 3):
            raise ValueError('Dynamic ELF requires unbundled host runtime')


# Windows OS DLLs are provided by the declared Windows10/11 baseline. Compiler
# redistributables (MSVCP/VCRUNTIME) deliberately are not treated as OS DLLs.
WINDOWS_SYSTEM_DLLS = set("kernel32.dll user32.dll advapi32.dll crypt32.dll bcrypt.dll iphlpapi.dll winmm.dll rpcrt4.dll ws2_32.dll shell32.dll ole32.dll oleaut32.dll ntdll.dll secur32.dll userenv.dll netapi32.dll psapi.dll dnsapi.dll dbghelp.dll winhttp.dll normaliz.dll version.dll shlwapi.dll gdi32.dll comdlg32.dll imm32.dll winspool.drv ucrtbase.dll".split())


def pe_imports(path):
    body = Path(path).read_bytes()
    try:
        pe = struct.unpack_from('<I', body, 0x3c)[0]
        if body[:2] != b'MZ' or body[pe:pe+4] != b'PE\0\0':
            raise ValueError('Expected Windows PE file')
        machine, sections = struct.unpack_from('<HH', body, pe+4)
        optional_size = struct.unpack_from('<H', body, pe+20)[0]
        optional = pe+24
        if machine != 0x8664 or struct.unpack_from('<H', body, optional)[0] != 0x20b:
            raise ValueError('Expected native x64 PE file')
        if optional_size < 224:
            raise ValueError('PE optional header too short for dependency inspection')
        if struct.unpack_from('<I', body, optional+216)[0] != 0:
            raise ValueError('Delay-load PE imports require additional reviewed dependency handling')
        import_rva = struct.unpack_from('<I', body, optional+120)[0]
        section_start = optional+optional_size
        def offset(rva):
            for i in range(sections):
                virtual_size, virtual_address, raw_size, raw_offset = struct.unpack_from('<IIII', body, section_start+i*40+8)
                if virtual_address <= rva < virtual_address+min(virtual_size, raw_size):
                    result = raw_offset+rva-virtual_address
                    if result < len(body):
                        return result
            raise ValueError('PE import RVA outside file')
        if import_rva == 0:
            return []
        imports = []
        start = offset(import_rva)
        for i in range(1024):
            record = struct.unpack_from('<IIIII', body, start+i*20)
            if record == (0, 0, 0, 0, 0):
                return imports
            name_start = offset(record[3])
            name_end = body.index(b'\0', name_start, min(len(body), name_start+260))
            name = body[name_start:name_end].decode('ascii').lower()
            if not re.fullmatch(r'[a-z0-9_.-]+', name):
                raise ValueError('Unsafe PE import name')
            imports.append(name)
        raise ValueError('Too many PE imports')
    except (struct.error, IndexError, UnicodeDecodeError) as error:
        raise ValueError('Malformed PE file') from error


def windows_dependencies(runtime):
    files = {p.name.lower(): p for p in Path(runtime).iterdir() if p.is_file()}
    missing = set()
    for path in files.values():
        if path.suffix.lower() not in ('.exe', '.dll'):
            continue
        for dependency in pe_imports(path):
            if dependency not in files and dependency not in WINDOWS_SYSTEM_DLLS and not dependency.startswith(('api-ms-win-', 'ext-ms-win-')):
                missing.add(dependency)
    if missing:
        raise ValueError('Missing reviewed app-local Windows dependencies: '+', '.join(sorted(missing)))


def runtime_extras(bundle, manifest, destination):
    for extra in manifest.get('runtimeExtraFiles', []):
        source = checked(owned_file(bundle, extra.get('file')), extra.get('sha256'))
        checked(owned_file(bundle, extra.get('licenseFile')), extra.get('licenseSha256'))
        name = source.name
        if not re.fullmatch(r'[A-Za-z0-9_.-]+\.dll', name) or (Path(destination)/name).exists():
            raise ValueError('Unsafe/colliding runtime extra file')
        shutil.copyfile(source, Path(destination)/name)


def tarball(stage, output):
    # Fixed metadata/order/timestamps and gzip header make repeated archives identical.
    with Path(output).open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode='w') as bundle:
            for path in [Path(stage), *sorted(Path(stage).rglob('*'))]:
                info = bundle.gettarinfo(str(path), arcname=str(Path('engelsiz') / path.relative_to(stage)))
                relative = path.relative_to(stage).as_posix()
                info.mode = 0o755 if path.is_dir() or relative in ('engelsiz', 'runtime/kiwix-serve', 'runtime/kiwix-search', 'runtime/kiwix-manage') else 0o644
                info.uid = info.gid = info.mtime = 0
                info.uname = info.gname = ''
                if path.is_symlink() or (not path.is_dir() and not path.is_file()):
                    raise ValueError('Unsafe staged release member')
                if path.is_file():
                    with path.open('rb') as body:
                        bundle.addfile(info, body)
                else:
                    bundle.addfile(info)


def run(*args, **kwargs):
    subprocess.run(args, check=True, cwd=ROOT, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', choices=['linux-amd64', 'windows-amd64'], required=True)
    parser.add_argument('--runtime-archive', type=Path)
    parser.add_argument('--download-runtime', action='store_true')
    parser.add_argument('--runtime-lock', type=Path, default=ROOT / 'third_party/runtime-lock.json',
                        help='Hash-pinned runtime inventory (defaults to the official runtime lock)')
    parser.add_argument('--compliance', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version', default='0.1.0')
    args = parser.parse_args()
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[A-Za-z0-9.]+)?', args.version):
        raise ValueError('Invalid release version')
    with ExitStack() as resources:
        inventories = json.loads(args.runtime_lock.read_text())
        if not isinstance(inventories, dict) or not isinstance(inventories.get(args.platform), dict):
            raise ValueError(f'Runtime lock has no inventory for {args.platform}')
        lock = inventories[args.platform]
        manifest = compliance(args.compliance, lock)
        if args.runtime_archive is None:
            if not args.download_runtime:
                raise ValueError('Supply --runtime-archive or --download-runtime')
            if not isinstance(lock.get('url'), str) or not lock['url']:
                raise ValueError('This runtime has no download URL; supply --runtime-archive')
            download_dir = resources.enter_context(tempfile.TemporaryDirectory(prefix='engelsiz-runtime-download-'))
            args.runtime_archive = Path(download_dir) / Path(lock['url']).name
            if not isinstance(lock.get('sha256'), str):
                raise ValueError('Missing reviewed runtime SHA-256 pin')
            urllib.request.urlretrieve(lock['url'], args.runtime_archive)
        checked(args.runtime_archive, lock['sha256'])
        output = args.output.resolve()
        if output.exists() and any(output.iterdir()):
            raise ValueError('Output directory must be empty')
        # Rebuild production assets; developer proxy is never set by this driver.
        npm = 'npm.cmd' if os.name == 'nt' else 'npm'
        subprocess.run([npm, 'ci'], check=True, cwd=ROOT / 'frontend')
        run(sys.executable, str(ROOT / 'scripts/collect-app-notices.py'), '--check')
        subprocess.run([npm, 'run', 'build'], check=True, cwd=ROOT / 'frontend')
        embedded = ROOT / 'internal/web/dist'
        for child in embedded.iterdir():
            if child.name != '.gitkeep':
                if child.is_dir():
                    shutil.rmtree(child)
                else:
                    child.unlink()
        shutil.copytree(ROOT / 'frontend/dist', embedded, dirs_exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='engelsiz-release-') as temporary:
            stage = Path(temporary) / 'stage'
            stage.mkdir()
            executable = extract_runtime(args.runtime_archive, lock, stage / 'runtime')
            target_os = args.platform.split('-')[0]
            if target_os == 'windows':
                runtime_extras(args.compliance, manifest, stage / 'runtime')
                windows_dependencies(stage / 'runtime')
            app = stage / ('engelsiz.exe' if target_os == 'windows' else 'engelsiz')
            env = {**os.environ, 'GOOS': target_os, 'GOARCH': 'amd64', 'GOAMD64': 'v1', 'CGO_ENABLED': '0'}
            run('go', 'build', '-trimpath', '-buildvcs=false', '-o', str(app), './cmd/engelsiz', env=env)
            if target_os == 'linux':
                static_elf(app)
                static_elf(executable)
            if target_os == ('windows' if os.name == 'nt' else 'linux'):
                result = subprocess.run([str(executable), '--version'], check=True, capture_output=True, text=True)
                if f'kiwix-tools {lock["version"]}' not in result.stdout:
                    raise ValueError('Unexpected runtime version')
            shutil.copytree(args.compliance, stage / 'LICENSES/runtime-corresponding-source')
            compliance(stage / 'LICENSES/runtime-corresponding-source', lock)
            shutil.copy2(ROOT / 'LICENSE', stage / 'LICENSES/Engelsiz-LICENSE')
            shutil.copy2(ROOT / 'third_party/README.md', stage / 'LICENSES/THIRD-PARTY-NOTICES.md')
            shutil.copy2(ROOT / f'packaging/{target_os}/README.txt', stage / 'README.txt')
            output.mkdir(parents=True, exist_ok=True)
            if target_os == 'linux':
                artifact = output / 'engelsiz-linux-x64.tar.gz'
                tarball(stage, artifact)
            else:
                run('iscc', f'/DStageDir={stage}', f'/DOutputDir={output}', f'/DAppVersion={args.version}', str(ROOT / 'packaging/windows/engelsiz.iss'))
                artifact = output / 'Engelsiz-Setup.exe'
                if not artifact.is_file():
                    raise ValueError('Installer compiler did not produce expected artifact')
            (output / (artifact.name + '.sha256')).write_text(f'{digest(artifact)}  {artifact.name}\n')
            print(artifact)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f'Release blocked: {error}') from error
