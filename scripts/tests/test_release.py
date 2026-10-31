import importlib.util
import io
import json
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

SPEC = importlib.util.spec_from_file_location('release', Path(__file__).parents[1] / 'release.py')
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ReleaseSafetyTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def archive(self, names):
        path = self.root / 'runtime.tar.gz'
        with tarfile.open(path, 'w:gz') as bundle:
            for name, kind in names:
                member = tarfile.TarInfo(name)
                member.type = kind
                member.size = 4 if kind == tarfile.REGTYPE else 0
                member.linkname = '../../outside' if kind == tarfile.SYMTYPE else ''
                bundle.addfile(member, io.BytesIO(b'tool') if member.size else None)
        return path

    def lock(self, archive):
        return {'sha256': release.digest(archive), 'root': 'tools', 'executable': 'kiwix-serve', 'version': 'test'}

    def app_notice_inventory(self, notice_body):
        directory = self.root / 'third_party/application-notices'
        directory.mkdir(parents=True, exist_ok=True)
        for name in ('go.mod', 'go.sum', 'frontend/package-lock.json', 'frontend/tailwind.config.js'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('Synthetic fixture input: ' + name + '\n')
        inventory = {'format': 1,
                     'inputs': {name: release.digest(self.root / name) for name in release.APPLICATION_NOTICE_INPUTS},
                     'outputs': {}}
        for scope in ('go', 'frontend'):
            notice = directory / (scope + '.txt')
            notice.write_bytes(notice_body)
            inventory['outputs'][scope + '.txt'] = {'sha256': release.digest(notice)}
        path = directory / 'inventory.json'
        path.write_text(json.dumps(inventory))
        return path

    def test_verified_runtime_layout_and_hash_rejection(self):
        path = self.archive([('tools/kiwix-serve', tarfile.REGTYPE)])
        executable = release.extract_runtime(path, self.lock(path), self.root / 'staged')
        self.assertEqual(executable.read_bytes(), b'tool')
        lock = self.lock(path)
        lock['sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'SHA-256'):
            release.extract_runtime(path, lock, self.root / 'bad')
        self.assertFalse((self.root / 'bad').exists())

    def test_traversal_links_and_duplicates_rejected(self):
        for names in [[('../outside', tarfile.REGTYPE)], [('tools/../../outside', tarfile.REGTYPE)], [('tools/link', tarfile.SYMTYPE)], [('tools/kiwix-serve', tarfile.REGTYPE)] * 2]:
            path = self.archive(names)
            with tempfile.TemporaryDirectory() as destination, self.assertRaises((ValueError, FileExistsError)):
                release.extract_runtime(path, self.lock(path), destination)
        for name in ['C:/outside', 'tools\\outside', '/absolute']:
            with self.assertRaises(ValueError):
                release.safe_member(name, 'tools')

    def test_windows_zip_preserves_runtime_dlls(self):
        path = self.root / 'runtime.zip'
        with zipfile.ZipFile(path, 'w') as bundle:
            bundle.writestr('tools/kiwix-serve.exe', b'PE')
            bundle.writestr('tools/needed.dll', b'DLL')
        lock = self.lock(path)
        lock['executable'] = 'kiwix-serve.exe'
        executable = release.extract_runtime(path, lock, self.root / 'windows')
        self.assertEqual(executable.read_bytes(), b'PE')
        self.assertEqual((executable.parent / 'needed.dll').read_bytes(), b'DLL')

    def test_compliance_fails_missing_license_manifest_sources_and_hash(self):
        bundle = self.root / 'compliance'
        bundle.mkdir()
        lock = {'sha256': '1' * 64, 'version': 'test', 'complianceManifestSha256': None}
        with self.assertRaisesRegex(ValueError, 'LICENSE'):
            release.compliance(bundle, lock, self.root)
        (self.root / 'LICENSE').write_text('Synthetic test license\n' * 10)
        with self.assertRaisesRegex(ValueError, 'SHA-256'):
            release.compliance(bundle, lock, self.root)
        (bundle / 'license.txt').write_text('Synthetic component license')
        (bundle / 'build.txt').write_text('Synthetic build instructions')
        (bundle / 'source.tar').write_bytes(b'synthetic source payload')
        data = {'runtimeSha256': '1' * 64, 'runtimeVersion': 'test', 'reviewedBy': 'synthetic unit test', 'reviewedAt': '2026-10-30', 'completeCorrespondingSource': True, 'components': [{'name': 'kiwix-tools', 'version': 'test', 'license': 'test', 'licenseFile': 'license.txt', 'licenseSha256': release.digest(bundle / 'license.txt'), 'sourceArchive': 'source.tar', 'sourceSha256': release.digest(bundle / 'source.tar'), 'buildInstructions': 'build.txt', 'buildInstructionsSha256': release.digest(bundle / 'build.txt')}]}
        data['applicationDependencies'] = {scope: {'noticeFile': 'license.txt', 'noticeSha256': release.digest(bundle / 'license.txt')} for scope in ['go', 'frontend']}
        manifest = bundle / 'manifest.json'
        manifest.write_text(json.dumps(data))
        lock['complianceManifestSha256'] = release.digest(manifest)
        self.app_notice_inventory((bundle / 'license.txt').read_bytes())
        release.compliance(bundle, lock, self.root)
        for filename in ['license.txt', 'build.txt', 'source.tar']:
            original = (bundle / filename).read_bytes()
            (bundle / filename).write_bytes(b'tampered')
            with self.assertRaisesRegex(ValueError, 'SHA-256'):
                release.compliance(bundle, lock, self.root)
            (bundle / filename).write_bytes(original)
        data.pop('applicationDependencies')
        manifest.write_text(json.dumps(data))
        lock['complianceManifestSha256'] = release.digest(manifest)
        with self.assertRaises(ValueError):
            release.compliance(bundle, lock, self.root)
        with self.assertRaises(ValueError):
            release.owned_file(bundle, '../LICENSE')

    def test_application_attribution_tracks_current_lockfiles_and_bundle(self):
        original = b'Synthetic original application attribution\n'
        inventory_path = self.app_notice_inventory(original)
        release.application_notice_inventory(self.root)
        for name in release.APPLICATION_NOTICE_INPUTS:
            path = self.root / name
            before = path.read_bytes()
            path.write_bytes(before + b'updated dependency/config input\n')
            with self.assertRaisesRegex(ValueError, 'stale'):
                release.application_notice_inventory(self.root)
            path.write_bytes(before)
        saved = inventory_path.read_bytes()
        inventory_path.unlink()
        with self.assertRaisesRegex(ValueError, 'Missing compliance file'):
            release.application_notice_inventory(self.root)
        inventory_path.write_bytes(saved)
        notice = inventory_path.parent / 'go.txt'
        notice.write_text('Changed local compendium without regenerating inventory')
        with self.assertRaisesRegex(ValueError, 'SHA-256'):
            release.application_notice_inventory(self.root)
        notice.write_bytes(original)

        # A newly pinned, internally consistent runtime manifest must still
        # reject an old app compendium after current app notices have changed.
        bundle = self.root / 'bundle'
        bundle.mkdir()
        (self.root / 'LICENSE').write_text('Synthetic project license\n' * 10)
        for name in ('license.txt', 'build.txt', 'source.tar'):
            (bundle / name).write_text('Synthetic component material\n')
        old_notice = bundle / 'old-app.txt'
        old_notice.write_text('Earlier app attribution\n')
        data = {'runtimeSha256': '1' * 64, 'runtimeVersion': 'test', 'reviewedBy': 'synthetic unit test',
                'reviewedAt': '2026-10-31', 'completeCorrespondingSource': True,
                'components': [{'name': 'kiwix-tools', 'version': 'test', 'license': 'test',
                    'licenseFile': 'license.txt', 'licenseSha256': release.digest(bundle / 'license.txt'),
                    'sourceArchive': 'source.tar', 'sourceSha256': release.digest(bundle / 'source.tar'),
                    'buildInstructions': 'build.txt', 'buildInstructionsSha256': release.digest(bundle / 'build.txt')}],
                'applicationDependencies': {scope: {'noticeFile': old_notice.name, 'noticeSha256': release.digest(old_notice)}
                                            for scope in ('go', 'frontend')}}
        manifest = bundle / 'manifest.json'
        manifest.write_text(json.dumps(data))
        lock = {'sha256': '1' * 64, 'version': 'test', 'complianceManifestSha256': release.digest(manifest)}
        with self.assertRaisesRegex(ValueError, 'Bundled go attribution is stale'):
            release.compliance(bundle, lock, self.root)
        old_notice.write_bytes(original)
        for scope in ('go', 'frontend'):
            data['applicationDependencies'][scope]['noticeSha256'] = release.digest(old_notice)
        manifest.write_text(json.dumps(data))
        lock['complianceManifestSha256'] = release.digest(manifest)
        release.compliance(bundle, lock, self.root)

    def test_static_elf_and_dynamic_rejection(self):
        header = bytearray(120)
        header[:6] = b'\x7fELF\x02\x01'
        struct.pack_into('<H', header, 18, 62)
        struct.pack_into('<Q', header, 32, 64)
        struct.pack_into('<HH', header, 54, 56, 1)
        struct.pack_into('<I', header, 64, 1)
        path = self.root / 'app'
        path.write_bytes(header)
        release.static_elf(path)
        for dynamic in [2, 3]:
            struct.pack_into('<I', header, 64, dynamic)
            path.write_bytes(header)
            with self.assertRaisesRegex(ValueError, 'Dynamic ELF'):
                release.static_elf(path)

    def test_tarball_repeatability_layout_and_modes(self):
        stage = self.root / 'stage'
        (stage / 'runtime').mkdir(parents=True)
        (stage / 'engelsiz').write_bytes(b'synthetic app')
        (stage / 'engelsiz').chmod(0o755)
        (stage / 'runtime/kiwix-serve').write_bytes(b'synthetic runtime')
        one, two = self.root / 'one.gz', self.root / 'two.gz'
        release.tarball(stage, one)
        stage.chmod(0o700)
        (stage / 'runtime').chmod(0o700)
        (stage / 'engelsiz').chmod(0o700)
        (stage / 'runtime/kiwix-serve').chmod(0o600)
        release.tarball(stage, two)
        self.assertEqual(release.digest(one), release.digest(two))
        with tarfile.open(one) as bundle:
            self.assertEqual(bundle.getmember('engelsiz/engelsiz').mode, 0o755)
            self.assertIn('engelsiz/runtime/kiwix-serve', bundle.getnames())

    def pe_fixture(self, path, dependency='kernel32.dll', delay=False):
        body = bytearray(1024)
        body[:2] = b'MZ'
        struct.pack_into('<I', body, 0x3c, 0x80)
        body[0x80:0x84] = b'PE\0\0'
        struct.pack_into('<HH', body, 0x84, 0x8664, 1)
        struct.pack_into('<H', body, 0x94, 240)
        struct.pack_into('<H', body, 0x98, 0x20b)
        struct.pack_into('<I', body, 0x98 + (216 if delay else 120), 0x1000)
        struct.pack_into('<IIII', body, 0x98+240+8, 512, 0x1000, 512, 512)
        struct.pack_into('<IIIII', body, 512, 0, 0, 0, 0x1040, 0)
        name = dependency.encode()+b'\0'
        body[576:576+len(name)] = name
        path.write_bytes(body)

    def test_missing_compiler_dll_and_delay_load_fail_closed(self):
        runtime = self.root / 'runtime'
        runtime.mkdir()
        app = runtime / 'kiwix-serve.exe'
        self.pe_fixture(app, 'vcruntime140.dll')
        with self.assertRaisesRegex(ValueError, 'vcruntime140.dll'):
            release.windows_dependencies(runtime)
        self.pe_fixture(runtime / 'vcruntime140.dll')
        release.windows_dependencies(runtime)
        self.pe_fixture(app, 'missing-delayed.dll', delay=True)
        with self.assertRaisesRegex(ValueError, 'Delay-load'):
            release.windows_dependencies(runtime)

    def test_acceptance_matrix_is_complete(self):
        text = (release.ROOT / 'docs/release-validation.md').read_text()
        for i in range(1, 19):
            self.assertIn(f'AC-{i:02}', text)
        for i in range(1, 17):
            self.assertRegex(text, rf'\| {i} [^\n]+\| PENDING \|')
        installer = (release.ROOT / 'packaging/windows/engelsiz.iss').read_text()
        self.assertIn('PrivilegesRequired=lowest', installer)
        self.assertNotIn('\n[UninstallDelete]', installer)


if __name__ == '__main__':
    unittest.main()
