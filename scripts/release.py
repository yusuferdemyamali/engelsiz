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
    data = json.loads(manifest_file.read_text(encoding='utf-8'))
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


def pmtiles_notices(root, lock, platform):
    # The official executable links many Go modules. Preserve their generated,
    # independently authenticated attribution, not just the upstream LICENSE.
    inventory = checked(Path(root) / 'inventory.json', lock.get('noticeInventorySha256'))
    document = json.loads(inventory.read_text(encoding='utf-8'))
    if (document.get('format') != 1 or document.get('archiveSha256', {}).get(platform) != lock['sha256'] or
            document.get('goModVerify') is not True or document.get('binaryModuleGraphMatchesSource') is not True or
            document.get('noticeSha256') != lock.get('noticeSha256')):
        raise ValueError('PMTiles attribution inventory does not match the pinned binary')
    checked(Path(root) / 'notices.txt', lock.get('noticeSha256'))


def map_asset_notices(stage, project=ROOT):
    # Map fonts+sprites license/source docs live in the embedded frontend.
    # Stage directly readable copies alongside both distributables.
    names = ('README.txt', 'fonts/OFL.txt', 'sprites/LICENSE.txt')
    destination = Path(stage) / 'LICENSES/map-assets'
    for name in names:
        source = owned_file(project, 'frontend/public/maps/' + name)
        target = destination / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
    return destination


def application_notice_inventory(project):
    """Bind bundled attribution to the app inputs being packaged now."""
    inventory_path = owned_file(project, 'third_party/application-notices/inventory.json')
    inventory = json.loads(inventory_path.read_text(encoding='utf-8'))
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
# Unversioned msvcrt.dll is the Windows system CRT used by MinGW, distinct from
# redistributable msvcr100.dll/msvcp140.dll. See Microsoft's explanation:
# https://devblogs.microsoft.com/oldnewthing/20140411-00/?p=1273
WINDOWS_SYSTEM_DLLS = set("kernel32.dll user32.dll advapi32.dll crypt32.dll bcrypt.dll iphlpapi.dll winmm.dll rpcrt4.dll ws2_32.dll shell32.dll ole32.dll oleaut32.dll ntdll.dll secur32.dll userenv.dll netapi32.dll psapi.dll dnsapi.dll dbghelp.dll winhttp.dll normaliz.dll version.dll shlwapi.dll gdi32.dll comdlg32.dll imm32.dll winspool.drv ucrtbase.dll imagehlp.dll wintrust.dll".split())
WINDOWS_SYSTEM_DLLS.add('msvcrt.dll')


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


TRANSLATION_RUNTIME_DIRNAME = 'translation'
TRANSLATION_RUNTIME_MANIFEST = 'translation-runtime-manifest.json'
TRANSLATION_RUNTIME_NOTICES = 'THIRD-PARTY-NOTICES'


def parse_glibc(text):
    """Extract (major, minor) from an ldd --version first line like
    'ldd (Ubuntu GLIBC 2.39-0ubuntu8.9) 2.39' or 'ldd (GNU libc) 2.44'."""
    if not isinstance(text, str):
        raise ValueError('Translation runtime manifest lacks a glibc floor')
    match = re.search(r'(\d+)\.(\d+)\s*$', text.strip())
    if not match:
        raise ValueError('Unparseable glibc floor in translation runtime manifest')
    return (int(match.group(1)), int(match.group(2)))


def elf_machine_x86_64(path):
    body = Path(path).read_bytes()
    if body[:6] != b'\x7fELF\x02\x01' or len(body) < 64 or struct.unpack_from('<H', body, 18)[0] != 62:
        raise ValueError('Expected Linux x86_64 ELF translation binary')
    return body


# Audited dynamic-linkage policy for the frozen translation runtime.
# static_elf still guards the app, kiwix and pmtiles binaries with zero
# exceptions; the worker binary is never passed through static_elf, it is
# audited here instead: exact PT_INTERP, every DT_NEEDED either a host system
# library from the declared baseline or a bundled library from the manifest
# allowlist, and a manifest glibc floor at or below the lock ceiling.
TRANSLATION_INTERP = '/lib64/ld-linux-x86-64.so.2'
TRANSLATION_SYSTEM_NEEDED = frozenset({
    'libc.so.6', 'libm.so.6', 'libpthread.so.0', 'libdl.so.2', 'librt.so.1',
    # The audited program interpreter itself (host baseline provides the
    # loader; some shared objects list its soname as DT_NEEDED).
    'ld-linux-x86-64.so.2',
})


def elf_program_headers(body):
    """Parse 64-bit little-endian ELF program headers.

    Returns a list of (p_type, p_offset, p_vaddr, p_filesz) tuples.
    """
    if body[:6] != b'\x7fELF\x02\x01' or len(body) < 64:
        raise ValueError('Expected Linux x86_64 ELF')
    offset = struct.unpack_from('<Q', body, 32)[0]
    size, count = struct.unpack_from('<HH', body, 54)
    if size < 56 or count > 64 or offset + size * count > len(body):
        raise ValueError('Invalid ELF program headers')
    headers = []
    for i in range(count):
        base = offset + i * size
        p_type, _, p_offset, p_vaddr, _, p_filesz, _, _ = struct.unpack_from('<IIQQQQQQ', body, base)
        headers.append((p_type, p_offset, p_vaddr, p_filesz))
    return headers


def elf_needed_libraries(body):
    """Extract (interp, [DT_NEEDED...]) from a 64-bit LE ELF binary."""
    headers = elf_program_headers(body)
    interp = None
    dynamic = None
    for p_type, p_offset, _, p_filesz in headers:
        if p_type == 3:  # PT_INTERP
            raw = body[p_offset:p_offset + p_filesz]
            interp = raw.split(b'\0', 1)[0].decode('ascii')
        elif p_type == 2:  # PT_DYNAMIC
            dynamic = (p_offset, p_filesz)
    if dynamic is None:
        raise ValueError('Translation binary lacks auditable dynamic linkage')
    # Shared objects legitimately have no PT_INTERP; only executables do.
    # Callers that require a loader check `interp` themselves.
    needed_offsets = []
    strtab_addr = strtab_size = None
    offset, filesz = dynamic
    if filesz % 16 or offset + filesz > len(body):
        raise ValueError('Invalid ELF dynamic section')
    for i in range(filesz // 16):
        tag, value = struct.unpack_from('<qQ', body, offset + i * 16)
        if tag == 1:  # DT_NEEDED
            needed_offsets.append(value)
        elif tag == 5:  # DT_STRTAB
            strtab_addr = value
        elif tag == 10:  # DT_STRSZ
            strtab_size = value
    if strtab_addr is None or strtab_size is None or strtab_size > 1 << 20:
        raise ValueError('Invalid ELF string table reference')
    strtab_off = None
    for p_type, p_offset, p_vaddr, p_filesz in headers:
        if p_type == 1 and p_vaddr <= strtab_addr < p_vaddr + p_filesz:  # PT_LOAD
            strtab_off = strtab_addr - p_vaddr + p_offset
    if strtab_off is None or strtab_off + strtab_size > len(body):
        raise ValueError('ELF string table outside loadable segments')
    strtab = body[strtab_off:strtab_off + strtab_size]
    needed = []
    for entry in needed_offsets:
        if entry >= len(strtab):
            raise ValueError('ELF needed-library reference outside string table')
        name = strtab[entry:].split(b'\0', 1)[0].decode('ascii')
        if not re.fullmatch(r'[A-Za-z0-9_.+-]+\.so(\.[0-9]+)*', name):
            raise ValueError(f'Unsafe ELF needed-library name: {name!r}')
        needed.append(name)
    return interp, needed


def audit_translation_dynamic(binary, allowlist):
    """Enforce the audited dynamic-linkage exception for the worker binary.

    Returns the audited DT_NEEDED list. Anything unexpected (wrong loader,
    unallowlisted dependency, missing linkage) fails closed; this function
    never weakens static_elf, which continues to guard every other binary.
    """
    body = elf_machine_x86_64(binary)
    interp, needed = elf_needed_libraries(body)
    if interp != TRANSLATION_INTERP:
        raise ValueError(f'Translation binary uses unexpected program interpreter: {interp}')
    allowed = set(allowlist) | set(TRANSLATION_SYSTEM_NEEDED)
    for name in needed:
        if name not in allowed:
            raise ValueError(f'Translation binary needs unallowlisted shared library: {name}')
    return needed


def audit_translation_shared_object(path):
    """Bundled .so files must be x86_64 ELF shared objects without their own
    program interpreter (no nested loader escapes the audit)."""
    body = Path(path).read_bytes()
    if body[:6] != b'\x7fELF\x02\x01' or len(body) < 64 or struct.unpack_from('<H', body, 18)[0] != 62:
        raise ValueError(f'Translation runtime shared library is not x86_64 ELF: {Path(path).name}')
    for p_type, _, _, _ in elf_program_headers(body):
        if p_type == 3:
            raise ValueError(f'Translation runtime shared library has its own interpreter: {Path(path).name}')


def audit_translation_transitive(body, name, present):
    """Every DT_NEEDED of a bundled ELF object must resolve to a host system
    library from the declared baseline or to a library file actually present
    in the runtime (not merely an allowlist string)."""
    _, needed = elf_needed_libraries(body)
    for dependency in needed:
        if dependency not in TRANSLATION_SYSTEM_NEEDED and dependency not in present:
            raise ValueError(f'{name} needs missing shared library: {dependency}')
    return needed


def translation_requirement_pins(project=ROOT):
    """Parse the committed, source-visible runtime requirements file.

    Returns {name: {'version': ..., 'hashes': [...]}}. The release validates
    the built manifest against these pins (plus the lock), never against
    self-asserted version strings alone.
    """
    requirements = Path(project) / 'scripts' / 'translation' / 'requirements-runtime.txt'
    text = requirements.read_text(encoding='utf-8')
    # Join backslash continuations before parsing requirement lines.
    text = re.sub(r'\\\n', ' ', text)
    pins = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith('#') or line.startswith('--'):
            continue
        match = re.fullmatch(r'([A-Za-z0-9_.-]+)==([A-Za-z0-9_.-]+)((?:\s+--hash=\S+)*)\s*(?:;\s*.+)?', line)
        if not match:
            raise ValueError(f'Unparseable translation requirements line: {line[:60]}')
        name, version, hashpart = match.group(1).lower(), match.group(2), match.group(3)
        hashes = []
        for token in re.findall(r'--hash=(\S+)', hashpart):
            # Normalize 'sha256:<hex>' to bare hex like the manifest records.
            hashes.append(token.split(':', 1)[1] if ':' in token else token)
        if not hashes:
            raise ValueError(f'Translation requirement lacks hash pins: {name}')
        pins[name] = {'version': version, 'hashes': hashes}
    if not pins:
        raise ValueError('Translation requirements file has no pinned packages')
    return pins


def translation_file_inventory(runtime_dir):
    """Map every staged runtime file (posix relative path) to (size, sha256).

    Rejects symlinks and special files. The manifest file itself is excluded
    (it cannot pin its own hash); everything else must be inventoried.
    """
    runtime_dir = Path(runtime_dir)
    inventory = {}
    for path in sorted(runtime_dir.rglob('*')):
        if path.is_symlink() or (not path.is_file() and not path.is_dir()):
            raise ValueError('Symlink/special file in translation runtime')
        if path.is_file():
            relative = path.relative_to(runtime_dir).as_posix()
            if relative == TRANSLATION_RUNTIME_MANIFEST:
                continue
            inventory[relative] = (path.stat().st_size, digest(path))
    if not inventory:
        raise ValueError('Translation runtime has no files to inventory')
    return inventory


def windows_dependencies_recursive(runtime, host_prerequisites=()):
    # Frozen translation runtimes keep DLLs in subdirectories (_internal);
    # every app-local PE anywhere under the runtime must resolve, same
    # fail-closed rule as windows_dependencies but recursive so no frozen
    # dependency escapes review. Native Python extensions (.pyd are DLLs)
    # are scanned too. Paths with spaces/Unicode are handled by
    # pathlib throughout (no shell quoting involved). Duplicate basenames
    # with different contents are rejected: silently shadowing a load path
    # is a supply-chain risk, not a resolution.
    # host_prerequisites names MSVC redistributables (vcruntime/msvcp) that
    # are a declared host prerequisite — the Microsoft VC++ 2015-2022
    # Redistributable (x64) — rather than bundled files. Anything else
    # missing still fails.
    files = {}
    for path in sorted(Path(runtime).rglob('*')):
        if path.is_symlink():
            raise ValueError('Symlink in translation runtime')
        if path.is_file() and path.suffix.lower() in ('.exe', '.dll', '.pyd'):
            key = path.name.lower()
            digest_here = digest(path)
            if key in files and files[key][1] != digest_here:
                raise ValueError(f'Duplicate Windows dependency with different contents: {path.name}')
            files.setdefault(key, (path, digest_here))
    missing = set()
    for path, _ in files.values():
        for dependency in pe_imports(path):
            if dependency in files or dependency in host_prerequisites:
                continue
            if dependency not in WINDOWS_SYSTEM_DLLS and not dependency.startswith(('api-ms-win-', 'ext-ms-win-')):
                missing.add(f'{path.name} -> {dependency}')
    if missing:
        raise ValueError('Missing reviewed app-local Windows dependencies: '+', '.join(sorted(missing)))


def stage_translation_runtime(runtime_dir, lock, platform, destination):
    """Verify a prebuilt frozen translation runtime and stage it.

    This is the explicit, audited exception to the static-ELF policy: the
    PyInstaller onedir runtime under runtime/translation is dynamically
    linked by construction (CPython + CTranslate2 + OpenBLAS). Only the exact
    binary named by the lock plus the manifest's shared-library allowlist may
    be dynamic; everything is hash/version-pinned and no wholesale bypass of
    static_elf (which still guards the app, kiwix and pmtiles binaries)
    occurs.
    """
    runtime_dir = Path(runtime_dir)
    manifest_path = runtime_dir / TRANSLATION_RUNTIME_MANIFEST
    if not manifest_path.is_file() or manifest_path.is_symlink():
        raise ValueError('Translation runtime manifest is missing')
    manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
    if manifest.get('format') != 1 or manifest.get('workerProtocol') != lock.get('workerProtocol'):
        raise ValueError('Translation runtime protocol/lock mismatch')
    if manifest.get('binary') != lock.get('binary'):
        raise ValueError('Translation runtime binary name is not the pinned one')
    for name, version in lock.get('packages', {}).items():
        if manifest.get('packages', {}).get(name) != version:
            raise ValueError(f'Translation runtime package pin mismatch: {name}')
    if not re.fullmatch(r'3\.11\.\d+', str(manifest.get('buildPython', ''))):
        raise ValueError('Translation runtime interpreter is not a pinned stable release (want 3.11.x final)')
    if not str(manifest.get('buildPython', '')).startswith(str(lock.get('buildPython', ''))):
        raise ValueError('Translation runtime interpreter is not the pinned one')
    # Cross-check the manifest against the committed requirements file, not
    # just its own version strings: versions must match the lock and every
    # recorded wheel hash must be a hash pinned in source.
    try:
        required = translation_requirement_pins()
    except (OSError, ValueError) as error:
        raise ValueError(f'Translation requirements pins unreadable: {error}') from error
    for name, version in lock.get('packages', {}).items():
        entry = required.get(name)
        if entry is None or entry['version'] != version:
            raise ValueError(f'Translation requirement pin mismatch: {name}')
        wheel_hashes = manifest.get('wheelPins', {}).get(name)
        if not wheel_hashes or not set(wheel_hashes) <= set(entry['hashes']):
            raise ValueError(f'Translation runtime wheel hashes are not source-pinned: {name}')
    binary = runtime_dir / lock['binary']
    if not binary.is_file() or binary.is_symlink() or binary.stat().st_size == 0:
        raise ValueError('Pinned translation runtime binary is missing')
    if not re.fullmatch(r'[a-f0-9]{64}', manifest.get('binarySha256') or ''):
        raise ValueError('Translation runtime manifest lacks a binary hash pin')
    if digest(binary) != manifest['binarySha256']:
        raise ValueError('Translation runtime binary does not match its manifest pin')
    notices = runtime_dir / TRANSLATION_RUNTIME_NOTICES
    if not notices.is_dir() or notices.is_symlink():
        raise ValueError('Translation runtime wheel licenses/notices are missing')
    if not any(notices.rglob('*')):
        raise ValueError('Translation runtime wheel licenses/notices are empty')
    target_os = platform.split('-')[0]
    # Full per-file inventory: the staged set must match the manifest exactly.
    # The bootloader binary hash alone is not sufficient; any modified,
    # deleted, or added file fails closed.
    actual = translation_file_inventory(runtime_dir)
    expected_entries = manifest.get('files')
    if not isinstance(expected_entries, list) or not expected_entries:
        raise ValueError('Translation runtime manifest lacks a file inventory')
    expected = {}
    for entry in expected_entries:
        if not isinstance(entry, dict) or not re.fullmatch(r'[A-Za-z0-9_+./ -]{1,200}', entry.get('path', '')):
            raise ValueError('Malformed translation runtime file inventory entry')
        if (entry.get('path') in expected or not isinstance(entry.get('size'), int)
                or not re.fullmatch(r'[a-f0-9]{64}', entry.get('sha256', ''))):
            raise ValueError('Malformed translation runtime file inventory entry')
        expected[entry['path']] = (entry['size'], entry['sha256'])
    if set(actual) != set(expected):
        missing = sorted(set(expected) - set(actual))
        unexpected = sorted(set(actual) - set(expected))
        raise ValueError(f'Translation runtime file set drift: missing={missing[:5]} unexpected={unexpected[:5]}')
    for relative, (size, sha256) in expected.items():
        observed_size, observed_sha = actual[relative]
        if observed_size != size or observed_sha != sha256:
            raise ValueError(f'Translation runtime file modified: {relative}')
    present_libraries = {Path(name).name for name in actual}
    if target_os == 'linux':
        allowlist = manifest.get('linuxSharedLibraryAllowlist')
        if not isinstance(allowlist, list) or not allowlist:
            raise ValueError('Translation runtime manifest lacks a shared library allowlist')
        needed = audit_translation_dynamic(binary, allowlist)
        for name in needed:
            if name not in TRANSLATION_SYSTEM_NEEDED and name not in present_libraries:
                raise ValueError(f'Translation binary needs missing shared library: {name}')
        allowed = set(allowlist)
        for path in runtime_dir.rglob('*'):
            if path.is_symlink() or (not path.is_file() and not path.is_dir()):
                raise ValueError('Symlink/special file in translation runtime')
            if path.is_file() and re.fullmatch(r'.*\.so(\.[0-9]+)*', path.name):
                if path.name not in allowed:
                    raise ValueError(f'Translation runtime shared library is not allowlisted: {path.name}')
                audit_translation_shared_object(path)
                audit_translation_transitive(path.read_bytes(), path.name, present_libraries)
        floor = parse_glibc(manifest.get('glibc'))
        ceiling = tuple(int(p) for p in str(lock.get('maxGlibc', '')).split('.'))
        if len(ceiling) != 2 or floor > ceiling:
            raise ValueError(f'Translation runtime glibc floor {manifest.get("glibc")} exceeds supported {lock.get("maxGlibc")}')
    elif target_os == 'windows':
        if binary.suffix.lower() != '.exe':
            raise ValueError('Expected Windows translation runtime executable')
        # MSVC redistributables (vcruntime/msvcp) are a declared host
        # prerequisite, never bundled: the manifest's hostPrerequisites must
        # list exactly the redistributable DLLs the tree imports, and only
        # redistributable-pattern names are accepted there.
        declared = manifest.get('hostPrerequisites')
        if not isinstance(declared, list) or not declared:
            raise ValueError('Translation runtime manifest lacks hostPrerequisites '
                             '(MSVC redistributables are a declared prerequisite, not bundled)')
        for name in declared:
            if not isinstance(name, str) or not re.fullmatch(
                    r'(vcruntime.*|msvcp.*|vcomp.*)\.dll', name.lower()):
                raise ValueError(f'Manifest hostPrerequisites must list only MSVC redistributables: {name!r}')
        windows_dependencies_recursive(runtime_dir, host_prerequisites={n.lower() for n in declared})
    else:
        raise ValueError('Unsupported translation runtime platform')
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    target = destination / TRANSLATION_RUNTIME_DIRNAME
    if target.exists():
        raise ValueError('Translation runtime staging target already exists')
    shutil.copytree(runtime_dir, target, symlinks=False)
    if target_os == 'linux':
        (target / lock['binary']).chmod(0o755)
    return target / lock['binary']


def tarball(stage, output):
    # Fixed metadata/order/timestamps and gzip header make repeated archives identical.
    with Path(output).open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode='w') as bundle:
            for path in [Path(stage), *sorted(Path(stage).rglob('*'))]:
                info = bundle.gettarinfo(str(path), arcname=str(Path('engelsiz') / path.relative_to(stage)))
                relative = path.relative_to(stage).as_posix()
                info.mode = 0o755 if path.is_dir() or relative in ('engelsiz', 'runtime/kiwix-serve', 'runtime/kiwix-search', 'runtime/kiwix-manage', 'runtime/pmtiles/pmtiles', 'runtime/translation/engelsiz-translate') else 0o644
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
    parser.add_argument('--pmtiles-archive', type=Path)
    parser.add_argument('--download-pmtiles', action='store_true')
    parser.add_argument('--translation-runtime', type=Path, required=True,
                        help='Prebuilt frozen translation runtime directory (fail-closed; never omitted)')
    parser.add_argument('--translation-runtime-lock', type=Path, default=ROOT / 'third_party/translation-runtime-lock.json',
                        help='Pinned translation runtime inventory (binary, protocol, packages, glibc ceiling)')
    parser.add_argument('--compliance', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version', default='1.0.0')
    args = parser.parse_args()
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[A-Za-z0-9.]+)?', args.version):
        raise ValueError('Invalid release version')
    with ExitStack() as resources:
        inventories = json.loads(args.runtime_lock.read_text(encoding='utf-8'))
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
        pmtiles_locks = json.loads((ROOT / 'third_party/pmtiles-lock.json').read_text(encoding='utf-8'))
        pmtiles_lock = pmtiles_locks[args.platform]
        pmtiles_notices(ROOT / 'third_party/pmtiles-notices', pmtiles_lock, args.platform)
        translation_locks = json.loads(args.translation_runtime_lock.read_text(encoding='utf-8'))
        if not isinstance(translation_locks, dict) or not isinstance(translation_locks.get(args.platform), dict):
            raise ValueError(f'Translation runtime lock has no inventory for {args.platform}')
        translation_lock = translation_locks[args.platform]
        if args.pmtiles_archive is None:
            if not args.download_pmtiles:
                raise ValueError('Supply --pmtiles-archive or --download-pmtiles')
            pmtiles_download = resources.enter_context(tempfile.TemporaryDirectory(prefix='engelsiz-pmtiles-download-'))
            args.pmtiles_archive = Path(pmtiles_download) / Path(pmtiles_lock['url']).name
            urllib.request.urlretrieve(pmtiles_lock['url'], args.pmtiles_archive)
        checked(args.pmtiles_archive, pmtiles_lock['sha256'])
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
            pmtiles_executable = extract_runtime(args.pmtiles_archive, pmtiles_lock, stage / 'runtime/pmtiles')
            translation_executable = stage_translation_runtime(args.translation_runtime, translation_lock, args.platform, stage / 'runtime')
            if not (stage / 'runtime/pmtiles/LICENSE').is_file():
                raise ValueError('PMTiles upstream license is missing')
            pmtiles_executable.chmod(0o755)
            target_os = args.platform.split('-')[0]
            if target_os == 'windows':
                runtime_extras(args.compliance, manifest, stage / 'runtime')
                windows_dependencies(stage / 'runtime')
                windows_dependencies(stage / 'runtime/pmtiles')
            app = stage / ('engelsiz.exe' if target_os == 'windows' else 'engelsiz')
            env = {**os.environ, 'GOOS': target_os, 'GOARCH': 'amd64', 'GOAMD64': 'v1', 'CGO_ENABLED': '0'}
            run('go', 'build', '-trimpath', '-buildvcs=false', '-o', str(app), './cmd/engelsiz', env=env)
            if target_os == 'linux':
                static_elf(app)
                static_elf(executable)
                static_elf(pmtiles_executable)
            if target_os == ('windows' if os.name == 'nt' else 'linux'):
                result = subprocess.run([str(executable), '--version'], check=True, capture_output=True, text=True, encoding='utf-8')
                if f'kiwix-tools {lock["version"]}' not in result.stdout:
                    raise ValueError('Unexpected runtime version')
                pmtiles_version = subprocess.run([str(pmtiles_executable), 'version'], check=True, capture_output=True, text=True, encoding='utf-8')
                if f'pmtiles {pmtiles_lock["version"]}' not in pmtiles_version.stdout:
                    raise ValueError('Unexpected PMTiles runtime version')
                smoke = subprocess.run([sys.executable, str(ROOT / 'scripts/translation/smoke.py'), '--binary', str(translation_executable)], capture_output=True, text=True, encoding='utf-8')
                if smoke.returncode != 0:
                    raise ValueError(f'Translation runtime smoke test failed: {(smoke.stderr or smoke.stdout).strip()[:300]}')
            shutil.copytree(args.compliance, stage / 'LICENSES/runtime-corresponding-source')
            compliance(stage / 'LICENSES/runtime-corresponding-source', lock)
            shutil.copytree(ROOT / 'third_party/pmtiles-notices', stage / 'LICENSES/pmtiles-notices')
            pmtiles_notices(stage / 'LICENSES/pmtiles-notices', pmtiles_lock, args.platform)
            shutil.copy2(ROOT / 'LICENSE', stage / 'LICENSES/Engelsiz-LICENSE')
            shutil.copy2(ROOT / 'third_party/README.md', stage / 'LICENSES/THIRD-PARTY-NOTICES.md')
            shutil.copy2(ROOT / 'third_party/assistant-notices.md', stage / 'LICENSES/assistant-notices.md')
            shutil.copy2(ROOT / 'docs/offline-ai.md', stage / 'AI-README.md')
            map_asset_notices(stage)
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
