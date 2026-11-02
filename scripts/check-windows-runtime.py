#!/usr/bin/env python3
"""Exercise a real Windows Kiwix runtime with a supplied indexed ZIM.

Maintainer tooling only. Wine results explicitly do not establish Windows
acceptance. Disconnect external networking separately for an offline test.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--runtime', type=Path, required=True)
    parser.add_argument('--zim', type=Path, required=True)
    parser.add_argument('--query', required=True)
    parser.add_argument('--runner', choices=('native', 'wine'), default='native')
    parser.add_argument('--wine-prefix', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.runner == 'native' and os.name != 'nt':
        parser.error('Native Windows execution requires Windows')
    if args.runner == 'wine' and args.wine_prefix is None:
        parser.error('Wine requires an explicit isolated --wine-prefix')
    runtime = args.runtime.resolve(strict=True)
    zim = args.zim.resolve(strict=True)
    env = dict(os.environ)
    if args.runner == 'wine':
        env.update(WINEPREFIX=str(args.wine_prefix.resolve()), WINEDEBUG='-all')
    report = {'host': platform.platform(), 'runner': args.runner,
              'cleanWindowsAcceptance': False,
              'externalNetworkingDisabled': 'not established by this tool',
              'runtimeSha256': sha(runtime), 'zimSha256': sha(zim),
              'query': args.query, 'runs': []}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='engelsiz-runtime-') as temporary:
        stage = Path(temporary) / 'Türkçe çalışma alanı'
        stage.mkdir()
        for source in runtime.parent.iterdir():
            if source.is_file() and source.suffix.lower() in ('.exe', '.dll'):
                shutil.copyfile(source, stage / source.name)
        content_id = '0123456789abcdef0123456789abcdef'
        content = stage / (content_id + '.zim')
        shutil.copyfile(zim, content)
        for attempt in range(2):
            with socket.socket() as reservation:
                reservation.bind(('127.0.0.1', 0))
                port = reservation.getsockname()[1]
            executable, zim_path = str(stage / runtime.name), str(content)
            command = []
            if args.runner == 'wine':
                command = ['wine']
                zim_path = subprocess.check_output(
                    ['winepath', '-w', zim_path], env=env, text=True).strip()
            command += [executable, '--address=127.0.0.1', f'--port={port}',
                        '--urlRootLocation=/content', zim_path]
            base = f'http://127.0.0.1:{port}'
            # Never inherit proxy settings for loopback validation.
            client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with (args.output.parent / f'runtime-{attempt}.log').open('wb') as log:
                process = subprocess.Popen(command, env=env, cwd=temporary,
                                           stdout=log, stderr=log)
                try:
                    for _ in range(150):
                        if process.poll() is not None:
                            raise RuntimeError(f'Runtime exited: {process.returncode}')
                        try:
                            with client.open(base + '/content/', timeout=1) as response:
                                if response.status == 200:
                                    break
                        except OSError:
                            time.sleep(0.2)
                    else:
                        raise RuntimeError('Runtime readiness timed out')
                    query = urllib.parse.urlencode({'content': content_id,
                                                    'pattern': args.query,
                                                    'format': 'xml', 'pageLength': 25})
                    with client.open(base + '/content/search?' + query, timeout=15) as response:
                        rss = response.read()
                    (args.output.parent / f'search-{attempt}.xml').write_bytes(rss)
                    document = ET.fromstring(rss)
                    count = document.findtext('./channel/{http://a9.com/-/spec/opensearch/1.1/}totalResults')
                    if not count or not count.isascii() or not count.isdecimal():
                        raise RuntimeError('OpenSearch count is not an ungrouped integer')
                    href = document.findtext('./channel/item/link')
                    if not href or not href.startswith('/content/content/' + content_id + '/'):
                        raise RuntimeError('Expected a real search result for the supplied ZIM')
                    with client.open(base + href, timeout=15) as response:
                        article = response.read()
                        if response.status != 200 or len(article) < 100:
                            raise RuntimeError('Article did not load')
                    report['runs'].append({'coldStart': attempt + 1,
                                           'totalResults': int(count),
                                           'articleHref': href,
                                           'articleBytes': len(article),
                                           'articleSha256': hashlib.sha256(article).hexdigest(),
                                           'unicodeAndSpacePath': True})
                finally:
                    if process.poll() is None:
                        process.terminate()
                    process.wait(timeout=15)
    report['passed'] = True
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(args.output)


if __name__ == '__main__':
    main()
