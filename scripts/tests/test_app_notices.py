import base64
import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('app_notices', Path(__file__).parents[1] / 'collect-app-notices.py')
notices = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(notices)


class ApplicationNoticeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def archive(self, content):
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode='w:gz') as archive:
            member = tarfile.TarInfo('package/LICENSE')
            member.size = len(content)
            archive.addfile(member, io.BytesIO(content))
        body = stream.getvalue()
        return body, {'resolved': 'https://registry.npmjs.org/example/-/example-1.0.0.tgz',
                      'integrity': 'sha512-' + base64.b64encode(hashlib.sha512(body).digest()).decode()}

    def test_exact_archive_and_notice_payload_are_authenticated(self):
        license_text = b'Copyright original author\nPermission and original conditions.\n'
        (self.root / 'LICENSE').write_bytes(license_text)
        body, lock = self.archive(license_text)
        with patch.object(notices.urllib.request, 'urlopen', return_value=io.BytesIO(body)):
            notices.verify_npm_archive(self.root, lock, {'LICENSE'})
        (self.root / 'LICENSE').write_text('modified attribution')
        with patch.object(notices.urllib.request, 'urlopen', return_value=io.BytesIO(body)), self.assertRaisesRegex(ValueError, 'Installed notice'):
            notices.verify_npm_archive(self.root, lock, {'LICENSE'})
        with patch.object(notices.urllib.request, 'urlopen', return_value=io.BytesIO(b'wrong archive')), self.assertRaisesRegex(ValueError, 'locked integrity'):
            notices.verify_npm_archive(self.root, lock, {'LICENSE'})

    def test_required_license_and_additional_notices_are_preserved(self):
        (self.root / 'README.md').write_text('No license is present')
        with self.assertRaisesRegex(ValueError, 'Missing license'):
            notices.root_notices(self.root)
        original = 'Copyright original author\nRedistribution conditions must stay intact.\n'
        (self.root / 'LICENSE').write_text(original)
        (self.root / 'NOTICE').write_text('Additional attribution\n')
        entries = notices.root_notices(self.root)
        rendered, inventory = notices.render('Synthetic component', [{'name': 'example', 'version': '1.0.0',
            'source': 'https://example.invalid/source', 'notices': entries}])
        self.assertIn(original.encode(), rendered)
        self.assertIn(b'Additional attribution\n', rendered)
        self.assertEqual(len(inventory[0]['noticeSources']), 2)
        self.assertNotIn('text', inventory[0]['noticeSources'][0])


if __name__ == '__main__':
    unittest.main()
