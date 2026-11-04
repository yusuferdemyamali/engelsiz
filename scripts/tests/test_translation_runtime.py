import ast
import importlib.util
import json
import os
import queue
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import threading
import unittest

SPEC = importlib.util.spec_from_file_location('release', Path(__file__).parents[1] / 'release.py')
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)

WORKER = Path(__file__).parents[1] / 'translation' / 'worker' / 'engelsiz_translate.py'
SMOKE = Path(__file__).parents[1] / 'translation' / 'smoke.py'


class PersistentReaderTests(unittest.TestCase):
    def test_short_request_returns_before_stdin_eof(self):
        source = ast.parse(WORKER.read_text(encoding='utf-8'))
        function = next(node for node in source.body
                        if isinstance(node, ast.FunctionDef) and node.name == '_make_reader')
        namespace = {'MAX_LINE_CHARS': 1024, 'MAX_DISCARD_CHARS': 4096}
        exec(compile(ast.Module(body=[function], type_ignores=[]), str(WORKER), 'exec'), namespace)
        read_fd, write_fd = os.pipe()
        reader = os.fdopen(read_fd, 'r', encoding='utf-8')
        writer = os.fdopen(write_fd, 'w', encoding='utf-8')
        results = queue.Queue()
        thread = threading.Thread(target=lambda: results.put(namespace['_make_reader'](reader)()), daemon=True)
        thread.start()
        try:
            writer.write('{"cmd":"translate"}\n')
            writer.flush()
            self.assertEqual(results.get(timeout=2), '{"cmd":"translate"}\n')
        finally:
            writer.close()
            thread.join(timeout=2)
            reader.close()


def translation_lock(platform='linux-amd64', binary='engelsiz-translate'):
    return {'binary': binary, 'workerProtocol': 1, 'buildPython': '3.11',
            'maxGlibc': '2.39',
            'packages': {'argostranslate': '1.11.0', 'ctranslate2': '4.8.5',
                         'sentencepiece': '0.2.2', 'numpy': '2.4.6',
                         'pyyaml': '6.0.3', 'packaging': '25.0'}}


def fake_elf(path):
    header = bytearray(120)
    header[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<H', header, 18, 62)
    struct.pack_into('<Q', header, 32, 64)
    struct.pack_into('<HH', header, 54, 56, 1)
    struct.pack_into('<IIQQQQQQ', header, 64, 1, 5, 0, 0x400000, 0x400000, 120, 120, 0x1000)
    path.write_bytes(header)


def fake_shared_elf(path, needed=('libc.so.6',)):
    """Shared library ELF: PT_LOAD + PT_DYNAMIC with DT_NEEDED, no INTERP."""
    names = [name.encode() + b'\0' for name in needed]
    strtab = b''.join(names)
    dynamic_entries = []
    string_offset = 0
    for name in names:
        dynamic_entries.append(struct.pack('<qQ', 1, string_offset))
        string_offset += len(name)
    dynamic_entries.append(struct.pack('<qQ', 5, 0x400000 + 0))  # patched below
    dynamic_entries.append(struct.pack('<qQ', 10, len(strtab)))
    dynamic_entries.append(struct.pack('<qQ', 0, 0))
    blob = b''.join(dynamic_entries)
    headers_size = 64 + 2 * 56
    dynamic_off = headers_size
    strtab_off = dynamic_off + len(blob)
    total = strtab_off + len(strtab)
    dynamic_entries[-3] = struct.pack('<qQ', 5, 0x400000 + strtab_off)
    blob = b''.join(dynamic_entries)
    body = bytearray(total)
    body[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<H', body, 18, 62)
    struct.pack_into('<Q', body, 32, 64)
    struct.pack_into('<HH', body, 54, 56, 2)
    struct.pack_into('<IIQQQQQQ', body, 64, 1, 5, 0, 0x400000, 0x400000, total, total, 0x1000)
    struct.pack_into('<IIQQQQQQ', body, 120, 2, 0, dynamic_off, 0, 0, len(blob), 0, 8)
    body[dynamic_off:dynamic_off + len(blob)] = blob
    body[strtab_off:strtab_off + len(strtab)] = strtab
    path.write_bytes(body)


def fake_dynamic_elf(path, needed=('libc.so.6', 'libz.so.1'),
                     interp='/lib64/ld-linux-x86-64.so.2'):
    """Minimal dynamic ELF: PT_INTERP + PT_LOAD + PT_DYNAMIC with DT_NEEDED."""
    interp_bytes = interp.encode() + b'\0'
    names = [name.encode() + b'\0' for name in needed]
    strtab = b''.join(names)
    dynamic_entries = []
    string_offset = 0
    for name in names:
        dynamic_entries.append(struct.pack('<qQ', 1, string_offset))
        string_offset += len(name)
    dynamic_entries.append(struct.pack('<qQ', 5, 0x400000 + 0))  # STRTAB patched below
    dynamic_entries.append(struct.pack('<qQ', 10, len(strtab)))
    dynamic_entries.append(struct.pack('<qQ', 0, 0))
    headers_size = 64 + 3 * 56
    interp_off = headers_size
    dynamic_off = interp_off + len(interp_bytes)
    strtab_off = dynamic_off + len(b''.join(dynamic_entries))
    total = strtab_off + len(strtab)
    # Patch the STRTAB virtual address now that the layout is known.
    dynamic_entries[-3] = struct.pack('<qQ', 5, 0x400000 + strtab_off)
    body = bytearray(total)
    body[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<H', body, 18, 62)
    struct.pack_into('<Q', body, 32, 64)
    struct.pack_into('<HH', body, 54, 56, 3)
    struct.pack_into('<IIQQQQQQ', body, 64, 3, 0, interp_off, 0, 0, len(interp_bytes), 0, 1)
    struct.pack_into('<IIQQQQQQ', body, 120, 1, 5, 0, 0x400000, 0x400000, total, total, 0x1000)
    struct.pack_into('<IIQQQQQQ', body, 176, 2, 0, dynamic_off, 0, 0, len(b''.join(dynamic_entries)), 0, 8)
    body[interp_off:interp_off + len(interp_bytes)] = interp_bytes
    body[dynamic_off:dynamic_off + len(b''.join(dynamic_entries))] = b''.join(dynamic_entries)
    body[strtab_off:strtab_off + len(strtab)] = strtab
    path.write_bytes(body)


class StageTranslationRuntimeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.runtime = self.root / 'translation-runtime-linux-amd64'
        self.runtime.mkdir()
        self.binary = self.runtime / 'engelsiz-translate'
        fake_dynamic_elf(self.binary)
        (self.runtime / 'THIRD-PARTY-NOTICES').mkdir()
        (self.runtime / 'THIRD-PARTY-NOTICES' / 'METADATA.txt').write_text('synthetic attribution')
        (self.runtime / '_internal').mkdir()
        fake_shared_elf(self.runtime / '_internal' / 'libz.so.1')
        self.manifest = {'format': 1, 'workerProtocol': 1, 'binary': 'engelsiz-translate',
                         'buildPython': '3.11.16',
                         'packages': dict(translation_lock()['packages']),
                         'binarySha256': release.digest(self.binary),
                         'glibc': 'ldd (Ubuntu GLIBC 2.39-0ubuntu8.9) 2.39',
                         'linuxSharedLibraryAllowlist': ['libz.so.1'],
                         'wheelPins': {name: info['hashes'][:1]
                                       for name, info in release.translation_requirement_pins().items()}}
        self.write_manifest()

    def write_manifest(self):
        self.manifest['binarySha256'] = release.digest(self.binary)
        inventory = release.translation_file_inventory(self.runtime)
        self.manifest['files'] = [
            {'path': path, 'size': size, 'sha256': sha256}
            for path, (size, sha256) in sorted(inventory.items())
        ]
        (self.runtime / 'translation-runtime-manifest.json').write_text(json.dumps(self.manifest))

    def stage(self):
        return release.stage_translation_runtime(self.runtime, translation_lock(), 'linux-amd64', self.root / 'stage' / 'runtime')

    def test_stage_sets_executable_and_manifest_pin(self):
        staged = self.stage()
        self.assertTrue(os.access(staged, os.X_OK))
        self.assertEqual(staged.parent.name, 'translation')
        self.assertTrue((staged.parent / 'translation-runtime-manifest.json').is_file())
        self.assertTrue((staged.parent / 'THIRD-PARTY-NOTICES' / 'METADATA.txt').is_file())

    def test_rejects_missing_runtime_dir(self):
        with self.assertRaises((ValueError, OSError)):
            release.stage_translation_runtime(self.root / 'absent', translation_lock(), 'linux-amd64', self.root / 'stage' / 'runtime')

    def test_rejects_package_version_and_protocol_mismatch(self):
        self.manifest['packages']['ctranslate2'] = '4.7.1'
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'pin mismatch'):
            self.stage()
        self.manifest['packages']['ctranslate2'] = '4.8.5'
        self.manifest['workerProtocol'] = 2
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'protocol'):
            self.stage()

    def test_rejects_binary_hash_and_name_mismatch(self):
        self.binary.write_bytes(b'tampered')
        with self.assertRaisesRegex(ValueError, 'manifest pin'):
            self.stage()
        fake_dynamic_elf(self.binary)
        self.manifest['binarySha256'] = release.digest(self.binary)
        self.manifest['binary'] = 'other-binary'
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'binary name'):
            self.stage()

    def test_rejects_glibc_above_ceiling_and_missing_notices(self):
        self.manifest['glibc'] = 'ldd (GNU libc) 2.44'
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'glibc floor'):
            self.stage()
        self.manifest['glibc'] = 'ldd (Ubuntu GLIBC 2.39-0ubuntu8.9) 2.39'
        self.write_manifest()
        for child in (self.runtime / 'THIRD-PARTY-NOTICES').iterdir():
            child.unlink()
        with self.assertRaisesRegex(ValueError, 'notices are empty'):
            self.stage()

    def test_rejects_unallowlisted_shared_library_and_symlink(self):
        evil = self.runtime / '_internal' / 'libevil.so.1'
        evil.write_bytes(b'ELF')
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'not allowlisted'):
            self.stage()
        evil.unlink()
        self.write_manifest()
        staged = self.stage()
        self.assertTrue((staged.parent / '_internal' / 'libz.so.1').is_file())
        outside = self.root / 'outside.txt'
        outside.write_text('outside')
        (self.runtime / '_internal' / 'libz.so.1').unlink()
        try:
            (self.runtime / '_internal' / 'link.so').symlink_to(outside)
        except OSError as error:
            if getattr(error, 'winerror', None) == 1314:
                self.skipTest('Creating symlinks requires Windows developer mode or privilege')
            raise
        with self.assertRaisesRegex(ValueError, 'Symlink'):
            release.stage_translation_runtime(self.runtime, translation_lock(), 'linux-amd64', self.root / 'stage2' / 'runtime')

    def test_rejects_unexpected_interp_and_unallowlisted_needed(self):
        fake_dynamic_elf(self.binary, interp='/lib/evil-ld.so')
        self.manifest['binarySha256'] = release.digest(self.binary)
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'unexpected program interpreter'):
            self.stage()
        fake_dynamic_elf(self.binary, needed=('libc.so.6', 'libevil.so.9'))
        self.manifest['binarySha256'] = release.digest(self.binary)
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'unallowlisted shared library: libevil.so.9'):
            self.stage()
        fake_dynamic_elf(self.binary)
        self.manifest['binarySha256'] = release.digest(self.binary)
        self.write_manifest()
        self.assertTrue(self.stage().is_file())

    def test_rejects_shared_object_with_own_interp(self):
        fake_dynamic_elf(self.runtime / '_internal' / 'libz.so.1')
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'own interpreter'):
            self.stage()

    def test_rejects_prerelease_interpreter(self):
        self.manifest['buildPython'] = '3.11.0rc1'
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'stable release'):
            self.stage()

    def test_rejects_modified_deleted_and_added_files(self):
        (self.runtime / '_internal').mkdir(exist_ok=True)
        extra = self.runtime / '_internal' / 'extra.dat'
        extra.write_bytes(b'synthetic payload')
        self.write_manifest()
        self.assertTrue(self.stage().is_file())
        extra.write_bytes(b'tampered payload!!')
        with self.assertRaisesRegex(ValueError, 'file modified'):
            release.stage_translation_runtime(self.runtime, translation_lock(), 'linux-amd64', self.root / 'stage-b' / 'runtime')
        extra.unlink()
        with self.assertRaisesRegex(ValueError, 'file set drift'):
            release.stage_translation_runtime(self.runtime, translation_lock(), 'linux-amd64', self.root / 'stage-c' / 'runtime')
        (self.runtime / 'unexpected.txt').write_text('intruder')
        with self.assertRaisesRegex(ValueError, 'file set drift'):
            release.stage_translation_runtime(self.runtime, translation_lock(), 'linux-amd64', self.root / 'stage-d' / 'runtime')

    def test_requirements_pins_match_lock(self):
        pins = release.translation_requirement_pins()
        for name, version in translation_lock()['packages'].items():
            self.assertIn(name, pins)
            self.assertEqual(pins[name]['version'], version)
            self.assertTrue(pins[name]['hashes'])

    def test_desymlink_materializes_and_rejects_escape(self):
        spec = importlib.util.spec_from_file_location(
            'translation_build', Path(__file__).parents[1] / 'translation' / 'build_runtime.py')
        build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(build)
        tree = self.root / 'frozen'
        (tree / 'sub').mkdir(parents=True)
        real = tree / 'sub' / 'libreal.so.1'
        real.write_bytes(b'synthetic library bytes')
        try:
            (tree / 'liblink.so.1').symlink_to(real)
        except OSError as error:
            if getattr(error, 'winerror', None) == 1314:
                self.skipTest('Creating symlinks requires Windows developer mode or privilege')
            raise
        build.desymlink_tree(tree)
        resolved = tree / 'liblink.so.1'
        self.assertFalse(resolved.is_symlink())
        self.assertEqual(resolved.read_bytes(), b'synthetic library bytes')
        try:
            (tree / 'escape.so').symlink_to(self.root / 'outside.txt')
        except OSError as error:
            if getattr(error, 'winerror', None) == 1314:
                self.skipTest('Creating symlinks requires Windows developer mode or privilege')
            raise
        (self.root / 'outside.txt').write_text('outside')
        with self.assertRaises(SystemExit):
            build.desymlink_tree(tree)

    def test_desymlink_tolerates_symlinked_root_prefix(self):
        # Windows TEMP may carry 8.3 short names (RUNNER~1) while resolve()
        # returns long names; resolving the root too keeps internal links
        # contained. A symlinked parent reproduces the mismatch portably.
        spec = importlib.util.spec_from_file_location(
            'translation_build_alias', Path(__file__).parents[1] / 'translation' / 'build_runtime.py')
        build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(build)
        real_root = self.root / 'realroot'
        (real_root / 'sub').mkdir(parents=True)
        real = real_root / 'sub' / 'libreal.so.1'
        real.write_bytes(b'synthetic library bytes')
        alias = self.root / 'aliasroot'
        try:
            alias.symlink_to(real_root, target_is_directory=True)
        except OSError as error:
            if getattr(error, 'winerror', None) == 1314:
                self.skipTest('Creating symlinks requires Windows developer mode or privilege')
            raise
        try:
            (alias / 'liblink.so.1').symlink_to(alias / 'sub' / 'libreal.so.1')
        except OSError as error:
            if getattr(error, 'winerror', None) == 1314:
                self.skipTest('Creating symlinks requires Windows developer mode or privilege')
            raise
        build.desymlink_tree(alias)
        resolved = alias / 'liblink.so.1'
        self.assertFalse(resolved.is_symlink())
        self.assertEqual(resolved.read_bytes(), b'synthetic library bytes')
        try:
            (alias / 'escape.so').symlink_to(self.root / 'outside.txt')
        except OSError as error:
            if getattr(error, 'winerror', None) == 1314:
                self.skipTest('Creating symlinks requires Windows developer mode or privilege')
            raise
        (self.root / 'outside.txt').write_text('outside')
        with self.assertRaises(SystemExit):
            build.desymlink_tree(alias)

    def test_parse_glibc(self):
        self.assertEqual(release.parse_glibc('ldd (Ubuntu GLIBC 2.39-0ubuntu8.9) 2.39'), (2, 39))
        self.assertEqual(release.parse_glibc('ldd (GNU libc) 2.35'), (2, 35))
        with self.assertRaises(ValueError):
            release.parse_glibc('no version here')
        with self.assertRaises(ValueError):
            release.parse_glibc(None)


class WindowsRecursiveDependencyTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def pe_fixture(self, path, dependency='kernel32.dll'):
        body = bytearray(1024)
        body[:2] = b'MZ'
        struct.pack_into('<I', body, 0x3c, 0x80)
        body[0x80:0x84] = b'PE\0\0'
        struct.pack_into('<HH', body, 0x84, 0x8664, 1)
        struct.pack_into('<H', body, 0x94, 240)
        struct.pack_into('<H', body, 0x98, 0x20b)
        struct.pack_into('<I', body, 0x98 + 120, 0x1000)
        struct.pack_into('<IIII', body, 0x98+240+8, 512, 0x1000, 512, 512)
        struct.pack_into('<IIIII', body, 512, 0, 0, 0, 0x1040, 0)
        name = dependency.encode()+b'\0'
        body[576:576+len(name)] = name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)

    def test_nested_dll_dependency_resolves_and_missing_fails(self):
        runtime = self.root / 'translation-links'
        self.pe_fixture(runtime / 'engelsiz-translate.exe', 'helper.dll')
        with self.assertRaisesRegex(ValueError, 'helper.dll'):
            release.windows_dependencies_recursive(runtime)
        self.pe_fixture(runtime / '_internal' / 'helper.dll')
        release.windows_dependencies_recursive(runtime)

    def test_pyd_extensions_are_scanned(self):
        runtime = self.root / 'translation-pyd'
        self.pe_fixture(runtime / 'engelsiz-translate.exe')
        self.pe_fixture(runtime / '_internal' / 'module.pyd', 'missing-native.dll')
        with self.assertRaisesRegex(ValueError, 'missing-native.dll'):
            release.windows_dependencies_recursive(runtime)

    def test_duplicate_basename_content_mismatch_rejected(self):
        runtime = self.root / 'translation-dup'
        self.pe_fixture(runtime / 'a' / 'helper.dll')
        self.pe_fixture(runtime / 'b' / 'helper.dll', 'extra.dll')
        with self.assertRaisesRegex(ValueError, '[Dd]uplicate'):
            release.windows_dependencies_recursive(runtime)
        (runtime / 'b' / 'helper.dll').write_bytes((runtime / 'a' / 'helper.dll').read_bytes())
        self.pe_fixture(runtime / 'engelsiz-translate.exe', 'helper.dll')
        release.windows_dependencies_recursive(runtime)

    def test_paths_with_spaces_and_unicode(self):
        runtime = self.root / 'çeviri runtime dir'
        self.pe_fixture(runtime / 'engelsiz-translate.exe', 'kernel32.dll')
        release.windows_dependencies_recursive(runtime)


class NativeNoticesWindowsTests(unittest.TestCase):
    """Synthetic Windows-layout fixtures for build_runtime.collect_native_notices.

    Covers the native-Windows attribution branch (CPython-bundled DLLs,
    --native-license overrides, wheel hash-mismatch fail-closed) without
    duplicating the release.windows_dependencies_recursive fixtures above.
    """

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        spec = importlib.util.spec_from_file_location(
            'translation_build_windows',
            Path(__file__).parents[1] / 'translation' / 'build_runtime.py')
        self.build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.build)
        # Stub the interpreter PSF lookup: every fixture runs offline with a
        # synthetic license; the real discovery path is exercised separately.
        self.build.find_interpreter_license = (
            lambda venv_python, override=None:
            'PYTHON SOFTWARE FOUNDATION LICENSE synthetic')
        # Stub the interpreter DLL hash set for determinism on Linux; tests
        # needing explicit hashes pass them, the real discovery test uses
        # self.real_dist_hashes.
        self.real_dist_hashes = self.build.interpreter_dist_hashes
        self.build.interpreter_dist_hashes = lambda venv_python: set()
        self.frozen = self.root / 'frozen'
        self.frozen.mkdir()
        (self.frozen / 'engelsiz-translate.exe').write_bytes(b'PE-stub')
        self.env_site = self.root / 'site'
        self.env_site.mkdir()
        self.notices = self.root / 'notices'
        self.notices.mkdir()
        self.papers = self.root / 'papers.txt'
        self.papers.write_text(
            'Synthetic license papers for fixture native libraries.\n'
            'Permission is hereby granted to use, copy, and distribute this\n'
            'synthetic fixture text for offline test attribution purposes.\n',
            encoding='utf-8')

    def collect(self, dist_hashes=None, **kwargs):
        kwargs.setdefault('venv_python', sys.executable)
        kwargs.setdefault('interp_dist_hashes',
                          set() if dist_hashes is None else dist_hashes)
        return self.build.collect_native_notices(
            self.frozen, 'engelsiz-translate.exe', self.env_site,
            self.notices, **kwargs)

    def test_cpython_bundled_dll_attributed_from_interpreter_papers(self):
        (self.frozen / '_internal').mkdir()
        (self.frozen / '_internal' / 'libcrypto-3.dll').write_bytes(b'fake-crypto-v1')
        (self.frozen / '_internal' / 'libssl-3.dll').write_bytes(b'fake-ssl-v1')
        (self.frozen / '_internal' / '_ssl.cp311-win_amd64.pyd').write_bytes(b'fake-ext')
        self.build.find_windows_bundled_license = (
            lambda venv_python, markers: self.papers)
        entries = self.collect()
        self.assertIn('_internal/libcrypto-3.dll', entries)
        self.assertIn('OpenSSL', entries['_internal/libcrypto-3.dll']['origin'])
        self.assertIn('_internal/libssl-3.dll', entries)
        self.assertEqual(entries['_internal/_ssl.cp311-win_amd64.pyd']['origin'],
                         'CPython 3.11 stdlib')
        self.assertTrue((self.notices / 'interpreter-LICENSE.txt').is_file())
        manifest = json.loads((self.notices / 'NATIVE-LIBS.json').read_text(encoding='utf-8'))
        self.assertEqual(manifest['_internal/libcrypto-3.dll']['origin'],
                         entries['_internal/libcrypto-3.dll']['origin'])

    def test_cpython_bundled_dll_missing_papers_fails_closed(self):
        (self.frozen / 'libcrypto-3.dll').write_bytes(b'fake-crypto-v1')
        self.build.find_windows_bundled_license = lambda venv_python, markers: None
        with self.assertRaisesRegex(SystemExit, r'lacks license papers.*--native-license libcrypto-3\.dll=PATH'):
            self.collect()

    def test_native_license_override_covers_unknown_dll_and_rejects_bare_url(self):
        (self.frozen / 'customvp.dll').write_bytes(b'fake-custom-v1')
        entries = self.collect(native_overrides=(f'customvp.dll={self.papers}',))
        self.assertEqual(entries['customvp.dll']['origin'],
                         'maintainer-supplied papers via --native-license')
        bare = self.root / 'bare.txt'
        bare.write_text('https://example.com/license\n', encoding='utf-8')
        with self.assertRaisesRegex(SystemExit, 'not license text'):
            self.collect(native_overrides=(f'customvp.dll={bare}',))
        with self.assertRaisesRegex(SystemExit, 'missing file'):
            self.collect(native_overrides=('customvp.dll=/nonexistent/papers.txt',))

    def test_wheel_hash_mismatch_fails_closed_never_misattributed_as_stdlib(self):
        import base64
        import hashlib
        payload = b'fake-extension-bytes'
        target = self.frozen / '_socket.cp311-win_amd64.pyd'
        target.write_bytes(payload)
        real_hex = hashlib.sha256(payload).hexdigest()
        wrong = hashlib.sha256(b'other-bytes').digest()
        wrong_b64 = base64.urlsafe_b64encode(wrong).decode().rstrip('=')
        dist = self.env_site / 'sockets-1.0.dist-info'
        dist.mkdir()
        (dist / 'METADATA').write_text('Name: sockets\nVersion: 1.0\n', encoding='utf-8')
        (dist / 'RECORD').write_text(
            f'_socket.cp311-win_amd64.pyd,sha256={wrong_b64},5\n', encoding='utf-8')
        with self.assertRaisesRegex(SystemExit, 'unattributed native library'):
            self.collect()
        self.assertNotEqual(
            self.build.record_index(self.env_site)['_socket.cp311-win_amd64.pyd'][0][2],
            real_hex)

    def test_space_unicode_paths_with_windows_layout(self):
        spaced = self.root / 'çeviri runtime dir' / 'frozen'
        spaced.mkdir(parents=True)
        (spaced / 'engelsiz-translate.exe').write_bytes(b'PE-stub')
        (spaced / 'sqlite3.dll').write_bytes(b'fake-sqlite-v1')
        notices = self.root / 'notices-spaced'
        notices.mkdir()
        self.build.find_windows_bundled_license = (
            lambda venv_python, markers: self.papers)
        entries = self.build.collect_native_notices(
            spaced, 'engelsiz-translate.exe', self.env_site, notices,
            venv_python=sys.executable)
        self.assertIn('sqlite3.dll', entries)
        self.assertIn('SQLite', entries['sqlite3.dll']['origin'])

    def test_find_windows_bundled_license_discovers_interpreter_file(self):
        interp_root = self.root / 'interp root ünïcode'
        interp_root.mkdir()
        (interp_root / 'LICENSE.txt').write_text(
            'Fake OpenSSL license bundle for discovery.\n'
            'This file documents the OpenSSL project terms shipped beside the\n'
            'interpreter for offline attribution testing purposes.\n',
            encoding='utf-8')
        stub = self._stub_python(str(interp_root), str(interp_root))
        text = '%s\n%s\n' % (interp_root, interp_root)
        found = self._run_with_stub(
            self.build.find_windows_bundled_license, stub, text, ('openssl',))
        self.assertIsNotNone(found)
        self.assertEqual(found, interp_root / 'LICENSE.txt')
        missing = self._run_with_stub(
            self.build.find_windows_bundled_license, stub, text,
            ('no-such-marker-xyz',))
        self.assertIsNone(missing)

    def test_plain_name_stdlib_pyd_attributed_via_interpreter_hash(self):
        import hashlib
        payloads = {
            '_ssl.pyd': b'fake-ssl-ext-plain-bytes',
            '_socket.pyd': b'fake-socket-ext-plain-bytes',
            '_hashlib.pyd': b'fake-hashlib-ext-plain-bytes',
            'select.pyd': b'fake-select-ext-plain-bytes',
        }
        for name, payload in payloads.items():
            (self.frozen / name).write_bytes(payload)
        dist_hashes = {hashlib.sha256(payload).hexdigest()
                       for payload in payloads.values()}
        entries = self.collect(dist_hashes=dist_hashes)
        for name in payloads:
            self.assertEqual(entries[name]['origin'], 'CPython 3.11 stdlib')
            self.assertEqual(entries[name]['license'], 'interpreter-LICENSE.txt')

    def test_plain_name_stdlib_blocked_by_wheel_hash_mismatch(self):
        import base64
        import hashlib
        payload = b'fake-socket-plain-ext-bytes'
        (self.frozen / '_socket.pyd').write_bytes(payload)
        real_hex = hashlib.sha256(payload).hexdigest()
        wrong_b64 = base64.urlsafe_b64encode(
            hashlib.sha256(b'other-bytes').digest()).decode().rstrip('=')
        dist = self.env_site / 'sockets-1.0.dist-info'
        dist.mkdir()
        (dist / 'METADATA').write_text('Name: sockets\nVersion: 1.0\n', encoding='utf-8')
        (dist / 'RECORD').write_text(
            f'_socket.pyd,sha256={wrong_b64},5\n', encoding='utf-8')
        # Even though the bytes match the interpreter distribution, the
        # mismatched wheel RECORD claim must fail closed, never misattribute.
        with self.assertRaisesRegex(SystemExit, 'unattributed native library'):
            self.collect(dist_hashes={real_hex})

    def test_interpreter_dist_hashes_from_base_prefix(self):
        import hashlib
        base = self.root / 'fakebase'
        (base / 'DLLs').mkdir(parents=True)
        ext = base / 'DLLs' / '_ssl.pyd'
        ext.write_bytes(b'fake-dist-ext')
        bundled = base / 'python311.dll'
        bundled.write_bytes(b'fake-dist-dll')
        (base / 'notes.txt').write_bytes(b'not a binary, must be ignored')
        stub = self._stub_python(str(base))
        found = self._run_with_stub(self.real_dist_hashes, stub, '%s\n' % base)
        self.assertEqual(found, {hashlib.sha256(b'fake-dist-ext').hexdigest(),
                                 hashlib.sha256(b'fake-dist-dll').hexdigest()})

    def _stub_python(self, *lines):
        """Platform-conditional stub executable printing lines on stdout.

        sh script on posix, .cmd batch (@echo off + echo lines) on
        Windows; plain sh stubs are not executable by subprocess on win32.
        """
        tag = getattr(self, '_stub_n', 0) + 1
        self._stub_n = tag
        if os.name == 'nt':
            stub = self.root / ('stub-%d.cmd' % tag)
            stub.write_text('@echo off\r\n' + ''.join('echo %s\r\n' % line for line in lines),
                            encoding='utf-8')
        else:
            stub = self.root / ('stub-%d' % tag)
            stub.write_text('#!/bin/sh\n' + ''.join('printf "%s\\n" "%s"\n' % (line, line) for line in lines),
                            encoding='utf-8')
            stub.chmod(0o755)
        return str(stub)

    def _run_with_stub(self, func, stub, text, *args):
        """Run a build helper against a stub executable.

        On Windows a .cmd cannot be executed directly by subprocess
        (WinError 193 without shell) and console echo would risk locale
        mangling of space/Unicode paths, so the stub's declared stdout is
        supplied via mock there; on posix the stub really executes. Either
        way func observes identical stdout text, so assertions match.
        """
        if os.name != 'nt':
            return func(stub, *args)
        import subprocess as _subprocess
        from unittest import mock
        real_run = _subprocess.run

        def fake_run(argv, **kwargs):
            if argv and argv[0] == stub:
                return _subprocess.CompletedProcess(argv, 0, stdout=text, stderr='')
            return real_run(argv, **kwargs)

        with mock.patch.object(self.build.subprocess, 'run', side_effect=fake_run):
            return func(stub, *args)

    def test_hashless_record_unique_vs_ambiguous(self):
        (self.frozen / 'libiomp5md.dll').write_bytes(b'fake-iomp-bytes')
        numpy_dist = self.env_site / 'numpy-2.4.6.dist-info'
        numpy_dist.mkdir()
        (numpy_dist / 'METADATA').write_text(
            'Name: numpy\nVersion: 2.4.6\n', encoding='utf-8')
        (numpy_dist / 'RECORD').write_text('libiomp5md.dll,,\n', encoding='utf-8')
        entries = self.collect()
        self.assertIn('numpy', entries['libiomp5md.dll']['origin'])
        other_dist = self.env_site / 'otherpkg-1.0.dist-info'
        other_dist.mkdir()
        (other_dist / 'METADATA').write_text(
            'Name: otherpkg\nVersion: 1.0\n', encoding='utf-8')
        (other_dist / 'RECORD').write_text('libiomp5md.dll,,\n', encoding='utf-8')
        with self.assertRaisesRegex(SystemExit, 'unattributed native library'):
            self.collect()


class WindowsBuildMechanicsTests(unittest.TestCase):
    """Fixtures for --native-dll injection, early PE audit, cuDNN strip."""

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        spec = importlib.util.spec_from_file_location(
            'translation_build_mechanics',
            Path(__file__).parents[1] / 'translation' / 'build_runtime.py')
        self.build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.build)

    def pe_fixture(self, path, dependency=None):
        body = bytearray(1024)
        body[:2] = b'MZ'
        struct.pack_into('<I', body, 0x3c, 0x80)
        body[0x80:0x84] = b'PE\0\0'
        struct.pack_into('<HH', body, 0x84, 0x8664, 1)
        struct.pack_into('<H', body, 0x94, 240)
        struct.pack_into('<H', body, 0x98, 0x20b)
        struct.pack_into('<I', body, 0x98 + 120, 0x1000 if dependency else 0)
        struct.pack_into('<IIII', body, 0x98+240+8, 512, 0x1000, 512, 512)
        struct.pack_into('<IIIII', body, 512, 0, 0, 0, 0x1040, 0)
        if dependency:
            name = dependency.encode() + b'\0'
            body[576:576+len(name)] = name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)

    def test_inject_copies_into_internal_and_rejects(self):
        frozen = self.root / 'frozen'
        frozen.mkdir()
        supply = self.root / 'supply'
        supply.mkdir()
        crt = supply / 'vcruntime140.dll'
        self.pe_fixture(crt)
        injected = self.build.inject_native_dlls(
            frozen, (f'vcruntime140.dll={crt}',))
        self.assertIn('vcruntime140.dll', injected)
        self.assertTrue((frozen / '_internal' / 'vcruntime140.dll').is_file())
        # Idempotent for identical contents.
        again = self.build.inject_native_dlls(
            frozen, (f'vcruntime140.dll={crt}',))
        self.assertEqual(again, injected)
        with self.assertRaisesRegex(SystemExit, '--native-dll needs NAME=PATH'):
            self.build.inject_native_dlls(frozen, ('bare-name',))
        with self.assertRaisesRegex(SystemExit, 'unsafe --native-dll name'):
            self.build.inject_native_dlls(frozen, ('../evil.dll=%s' % crt,))
        with self.assertRaisesRegex(SystemExit, 'not a regular file'):
            self.build.inject_native_dlls(
                frozen, ('missing.dll=%s' % (supply / 'absent.dll'),))
        fake_system32 = self.root / 'Windows' / 'System32'
        fake_system32.mkdir(parents=True)
        system_dll = fake_system32 / 'vcruntime140.dll'
        system_dll.write_bytes(b'system32 bytes that must never be harvested')
        with self.assertRaisesRegex(SystemExit, 'System32'):
            self.build.inject_native_dlls(frozen, ('vcruntime140.dll=%s' % system_dll,))
        other = supply / 'other-crt.dll'
        other.write_bytes(b'different contents that must not shadow silently')
        with self.assertRaisesRegex(SystemExit, 'different contents'):
            self.build.inject_native_dlls(frozen, ('vcruntime140.dll=%s' % other,))

    def test_audit_fails_early_and_passes_after_inject(self):
        frozen = self.root / 'frozen-audit'
        self.pe_fixture(frozen / 'engelsiz-translate.exe', 'helper.dll')
        with self.assertRaisesRegex(SystemExit, 'missing app-local'):
            self.build.audit_windows_dependencies(frozen)
        supply = self.root / 'supply2'
        supply.mkdir()
        helper = supply / 'helper.dll'
        self.pe_fixture(helper, 'kernel32.dll')
        self.build.inject_native_dlls(frozen, ('helper.dll=%s' % helper,))
        self.build.audit_windows_dependencies(frozen)
        # OS-baseline imports alone always pass; duplicates with different
        # contents fail closed.
        self.pe_fixture(frozen / 'other' / 'helper.dll', 'user32.dll')
        with self.assertRaisesRegex(SystemExit, '[Dd]uplicate'):
            self.build.audit_windows_dependencies(frozen)

    def test_cudnn_stripped_for_cpu_only(self):
        frozen = self.root / 'frozen-gpu'
        (frozen / '_internal').mkdir(parents=True)
        (frozen / '_internal' / 'cudnn64_9.dll').write_bytes(b'gpu accident')
        (frozen / '_internal' / 'CUDNN64_8.dll').write_bytes(b'gpu accident 8')
        keeper = frozen / '_internal' / 'libiomp5md.dll'
        keeper.write_bytes(b'cpu library that must stay')
        removed = self.build.strip_gpu_libraries(frozen)
        self.assertEqual(sorted(removed),
                         ['_internal/CUDNN64_8.dll', '_internal/cudnn64_9.dll'])
        self.assertFalse((frozen / '_internal' / 'cudnn64_9.dll').exists())
        self.assertTrue(keeper.is_file())
        self.assertEqual(self.build.strip_gpu_libraries(frozen), [])

    def test_api_set_forwarders_stripped_not_bundled(self):
        frozen = self.root / 'frozen-apiset'
        (frozen / '_internal').mkdir(parents=True)
        (frozen / '_internal' / 'api-ms-win-core-console-l1-1-0.dll').write_bytes(b'forwarder')
        (frozen / '_internal' / 'ext-ms-win-ntuser-draw-l1-1-0.dll').write_bytes(b'forwarder')
        keeper = frozen / '_internal' / 'libiomp5md.dll'
        keeper.write_bytes(b'cpu library that must stay')
        removed = self.build.strip_api_set_libraries(frozen)
        self.assertEqual(sorted(removed),
                         ['_internal/api-ms-win-core-console-l1-1-0.dll',
                          '_internal/ext-ms-win-ntuser-draw-l1-1-0.dll'])
        self.assertTrue(keeper.is_file())
        self.assertEqual(self.build.strip_api_set_libraries(frozen), [])

    def test_msvc_redistributables_stripped_as_host_prerequisite(self):
        frozen = self.root / 'frozen-msvc'
        (frozen / '_internal').mkdir(parents=True)
        (frozen / '_internal' / 'MSVCP140.dll').write_bytes(b'redist copy')
        (frozen / '_internal' / 'vcruntime140_1.dll').write_bytes(b'redist copy')
        keeper = frozen / '_internal' / 'libiomp5md.dll'
        keeper.write_bytes(b'cpu library that must stay')
        removed = self.build.strip_msvc_redistributables(frozen)
        self.assertEqual(sorted(removed),
                         ['_internal/MSVCP140.dll',
                          '_internal/vcruntime140_1.dll'])
        self.assertTrue(keeper.is_file())
        self.assertEqual(self.build.strip_msvc_redistributables(frozen), [])

    def test_os_baseline_dll_copy_stripped_not_attributed(self):
        frozen = self.root / 'frozen-osbaseline'
        (frozen / '_internal').mkdir(parents=True)
        (frozen / '_internal' / 'ucrtbase.dll').write_bytes(b'os copy')
        keeper = frozen / '_internal' / 'libiomp5md.dll'
        keeper.write_bytes(b'cpu library that must stay')
        removed = self.build.strip_os_baseline_libraries(frozen)
        self.assertEqual(removed, ['_internal/ucrtbase.dll'])
        self.assertTrue(keeper.is_file())
        self.assertEqual(self.build.strip_os_baseline_libraries(frozen), [])


def worker_command():
    """Return ([binary...]) for protocol tests, or None (skip).

    Prefers ENGELSIZ_TRANSLATION_TEST_BINARY (frozen binary under test);
    otherwise runs the worker source with the current interpreter when the
    real argostranslate stack is importable.
    """
    binary = os.environ.get('ENGELSIZ_TRANSLATION_TEST_BINARY')
    if binary:
        return [binary]
    try:
        import argostranslate  # noqa: F401
    except ImportError:
        return None
    return [sys.executable, str(WORKER)]


def run_protocol(command, models_dir, requests, timeout=300):
    process = subprocess.Popen(
        [*command, str(models_dir)], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL, text=True, encoding='utf-8')
    try:
        stdout, _ = process.communicate('\n'.join(requests) + '\n', timeout=timeout)
    except subprocess.TimeoutExpired:
        process.kill()
        raise
    process.wait(timeout=30)
    return process.returncode, stdout


class TranslationWorkerProtocolTests(unittest.TestCase):
    def setUp(self):
        self.command = worker_command()
        if self.command is None:
            self.skipTest('argostranslate is not installed for this interpreter; '
                          'set ENGELSIZ_TRANSLATION_TEST_BINARY to a frozen runtime')
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.models = Path(self.temporary.name)

    def talk(self, requests):
        code, stdout = run_protocol(self.command, self.models, requests)
        self.assertEqual(code, 0)
        lines = [line for line in stdout.splitlines() if line.strip()]
        self.assertEqual(len(lines), len(requests))
        return [json.loads(line) for line in lines]

    def test_invalid_and_missing_model_fail_closed(self):
        responses = self.talk([
            'not json',
            json.dumps({'cmd': 'translate', 'source_language': 'tr'}),
            json.dumps({'cmd': 'translate', 'source_language': 'tr', 'target_language': 'tr', 'text': 'x'}),
            json.dumps({'cmd': 'translate', 'source_language': 'tr', 'target_language': 'en', 'text': 'Merhaba'}),
            json.dumps({'cmd': 'translate', 'source_language': 'tr', 'target_language': 'en', 'text': 'x' * 5001}),
            json.dumps({'cmd': 'translate', 'source_language': 'tr', 'target_language': 'en', 'text': ''}),
        ])
        self.assertEqual(responses[0]['error']['code'], 'invalid_request')
        self.assertEqual(responses[1]['error']['code'], 'invalid_request')
        self.assertEqual(responses[2]['error']['code'], 'unsupported_pair')
        self.assertEqual(responses[3]['error']['code'], 'model_missing')
        self.assertEqual(responses[4]['error']['code'], 'text_too_long')
        self.assertEqual(responses[5], {'translated_text': ''})
        for response in responses[:5]:
            self.assertNotIn('Merhaba', json.dumps(response, ensure_ascii=False))

    def test_oversized_protocol_line_answered_and_resyncs(self):
        responses = self.talk([
            'y' * 70000,
            json.dumps({'cmd': 'translate', 'source_language': 'tr', 'target_language': 'en', 'text': 'Merhaba'}),
        ])
        self.assertEqual(responses[0]['error']['code'], 'invalid_request')
        self.assertEqual(responses[1]['error']['code'], 'model_missing')

    def test_real_models_translate_both_directions(self):
        models = os.environ.get('ENGELSIZ_TRANSLATION_TEST_MODELS')
        if not models:
            self.skipTest('Set ENGELSIZ_TRANSLATION_TEST_MODELS to a models root for live translation')
        code, stdout = run_protocol(self.command, models, [
            json.dumps({'cmd': 'translate', 'source_language': 'tr', 'target_language': 'en', 'text': 'Merhaba dünya.'}),
            json.dumps({'cmd': 'translate', 'source_language': 'en', 'target_language': 'tr', 'text': 'Hello world.'}),
            json.dumps({'cmd': 'translate', 'source_language': 'tr', 'target_language': 'en', 'text': 'Birinci satır.\n\nÜçüncü satır.'}),
        ])
        self.assertEqual(code, 0)
        responses = [json.loads(line) for line in stdout.splitlines() if line.strip()]
        self.assertEqual(len(responses), 3)
        self.assertIn('Hello', responses[0]['translated_text'])
        self.assertTrue(responses[1]['translated_text'].strip())
        self.assertEqual(responses[2]['translated_text'].count('\n'), 2)


if __name__ == '__main__':
    unittest.main()
