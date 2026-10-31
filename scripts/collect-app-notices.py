#!/usr/bin/env python3
"""Collect locked app attribution material; does not certify runtime compliance."""
import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / 'third_party/application-notices'
ROOT_NOTICE = re.compile(r'^(?:licen[sc]e|copying|copyright|notice|patents|authors|contributors)(?:$|[-_.])', re.I)
COMMENT = re.compile(r'/\*.*?\*/|(?m:(?:^[ \t]*//[^\n]*(?:\n|$))+)', re.S)
GENERATED_OUTPUT = {
    'tailwindcss': 'generated CSS preflight/utilities',
    'tailwindcss-animate': 'generated animation CSS',
    'vite': 'conservative inclusion for generated module-preload helper; full published license retained',
    'esbuild': 'conservative inclusion for generated bundler helpers',
}


def sha(body):
    return hashlib.sha256(body).hexdigest()


def json_stream(text):
    decoder = json.JSONDecoder()
    position = 0
    while position < len(text):
        while position < len(text) and text[position].isspace():
            position += 1
        if position == len(text):
            break
        value, position = decoder.raw_decode(text, position)
        yield value


def root_notices(directory):
    entries = []
    for path in sorted(Path(directory).iterdir()):
        if path.is_file() and ROOT_NOTICE.match(path.name):
            body = path.read_bytes()
            if body:
                entries.append({'path': path.name, 'sha256': sha(body), 'text': body.decode('utf-8')})
    if not any(ROOT_NOTICE.match(entry['path']) and entry['path'].lower().startswith(('license', 'licence', 'copying', 'copyright')) for entry in entries):
        raise ValueError(f'Missing license material in {directory}')
    return entries


def run_go(go, args, platform=None):
    env = {**os.environ, 'GOTOOLCHAIN': 'local', 'CGO_ENABLED': '0', 'GOARCH': 'amd64', 'GOAMD64': 'v1'}
    if platform:
        env['GOOS'] = platform
    return subprocess.check_output([go, *args], cwd=ROOT, env=env, text=True, encoding='utf-8')


def go_components(go):
    goroot = Path(run_go(go, ['env', 'GOROOT']).strip())
    version = run_go(go, ['env', 'GOVERSION']).strip()
    expected = re.search(r'^go (\S+)', (ROOT / 'go.mod').read_text(encoding='utf-8'), re.M).group(1)
    if version != 'go' + expected:
        raise ValueError(f'Collect with the release toolchain go{expected}, got {version}')
    modules, packages, per_platform = {}, {}, {}
    for platform in ('linux', 'windows'):
        selected = list(json_stream(run_go(go, ['list', '-buildvcs=false', '-deps', '-json', './cmd/engelsiz'], platform)))
        per_platform[platform + '-amd64'] = sorted(item['ImportPath'] for item in selected)
        for package in selected:
            if package.get('Module', {}).get('Main'):
                continue
            packages[(package['ImportPath'], platform)] = package
            module = package.get('Module')
            if module:
                if module.get('Replace'):
                    raise ValueError('Replaced production Go module needs explicit provenance review')
                modules[module['Path']] = module
    # Verify after go list has populated a cold cache, before reading any of
    # those source directories. This also authenticates first-run CI inputs.
    run_go(go, ['mod', 'verify'])
    components = [{'name': 'Go runtime and standard library', 'version': version,
                   'source': f'https://github.com/golang/go/tree/{version}',
                   'scope': ['linux-amd64', 'windows-amd64'],
                   'notices': root_notices(goroot)}]
    # Preserve licenses/patent grants for stdlib-vendored x/* code actually used.
    vendors = set()
    for package in packages.values():
        if package['ImportPath'].startswith('vendor/golang.org/x/'):
            vendors.add('/'.join(package['ImportPath'].split('/')[:4]))
    for vendor in sorted(vendors):
        components.append({'name': vendor.removeprefix('vendor/'), 'version': f'vendored in {version}',
                           'source': f'https://github.com/golang/go/tree/{version}/src/{vendor}',
                           'scope': ['linux-amd64', 'windows-amd64'], 'notices': root_notices(goroot / 'src' / vendor)})
    for name, module in sorted(modules.items()):
        components.append({'name': name, 'version': module['Version'],
                           'source': f'https://proxy.golang.org/{name}/@v/{module["Version"]}.zip',
                           'moduleSum': module.get('Sum'),
                           'scope': [platform for platform, imports in per_platform.items()
                                     if any(p == name or p.startswith(name + '/') for p in imports)],
                           'notices': root_notices(module['Dir'])})
    # Additional notices can live in source comments (e.g. Sun fdlibm math).
    # Preserve full comment blocks, without inferring their license or linking
    # every package in go list -m all into the shipped program.
    blocks = {}
    for package in packages.values():
        module = package.get('Module')
        directory = Path(package['Dir'])
        base = Path(module['Dir']) if module else goroot
        label = f'{module["Path"]}@{module["Version"]}' if module else version
        for filename in package.get('GoFiles', []) + package.get('SFiles', []):
            path = directory / filename
            body = path.read_bytes()
            for match in COMMENT.finditer(body.decode('utf-8')):
                block = match.group(0)
                if 'copyright' not in block.lower():
                    continue
                key = sha(block.encode())
                entry = blocks.setdefault(key, {'path': 'inline copyright block ' + key,
                                               'sha256': key, 'text': block, 'origins': []})
                origin = {'source': f'{label}/{path.relative_to(base).as_posix()}', 'sourceSha256': sha(body)}
                if origin not in entry['origins']:
                    entry['origins'].append(origin)
    for entry in blocks.values():
        entry['origins'].sort(key=lambda origin: origin['source'])
    components.append({'name': 'Inline notices from selected Go source/assembly', 'version': version,
                       'source': 'Exact production go list -deps source files, both platforms',
                       'notices': [blocks[key] for key in sorted(blocks)]})
    return components, per_platform


def frontend_components():
    lock = json.loads((ROOT / 'frontend/package-lock.json').read_text(encoding='utf-8'))
    components, excluded = [], []
    for location, info in sorted(lock['packages'].items()):
        if not location:
            continue
        name = location.split('node_modules/')[-1]
        if info.get('dev') and name not in GENERATED_OUTPUT:
            excluded.append({'name': name, 'version': info['version'], 'reason': 'build-only dependency; not included by this notice scope'})
            continue
        directory = ROOT / 'frontend' / location
        installed = json.loads((directory / 'package.json').read_text(encoding='utf-8'))
        if installed['name'] != name or installed['version'] != info['version']:
            raise ValueError(f'Installed frontend package differs from package-lock: {name}')
        notices = root_notices(directory)
        authenticated_files = {notice['path'] for notice in notices}
        # Apache-2.0 cva's attribution appears in its source header rather than
        # its generic Apache LICENSE. Keep all public entry-point headers too.
        for relative in ('dist/index.js', 'index.js'):
            path = directory / relative
            if path.is_file():
                for match in COMMENT.finditer(path.read_text(encoding='utf-8')):
                    block = match.group(0)
                    if 'copyright' in block.lower():
                        notices.append({'path': relative + ' copyright header', 'sha256': sha(block.encode()), 'text': block})
                        authenticated_files.add(relative)
        verify_npm_archive(directory, info, authenticated_files)
        components.append({'name': name, 'version': info['version'], 'declaredLicense': info.get('license'),
                           'source': info.get('resolved'), 'archiveIntegrity': info.get('integrity'),
                           'noticePayloadsVerifiedAgainstArchive': True,
                           'scope': GENERATED_OUTPUT.get(name, 'locked production dependency (conservative superset of tree-shaken bundle)'),
                           'notices': notices})
    return components, excluded


def verify_npm_archive(directory, info, filenames):
    """Authenticate selected notice/source bytes against the lockfile tarball."""
    source = info.get('resolved', '')
    if not source.startswith('https://'):
        raise ValueError('Expected locked HTTPS npm archive')
    integrity = info.get('integrity', '')
    algorithm, encoded = integrity.split('-', 1)
    if algorithm not in ('sha512', 'sha256'):
        raise ValueError('Unsupported npm archive integrity algorithm')
    with urllib.request.urlopen(source, timeout=30) as response:
        body = response.read(64 * 1024 * 1024 + 1)
    if len(body) > 64 * 1024 * 1024:
        raise ValueError('npm archive exceeds notice collector limit')
    actual = base64.b64encode(hashlib.new(algorithm, body).digest()).decode()
    if actual != encoded:
        raise ValueError('npm archive differs from locked integrity')
    with tarfile.open(fileobj=io.BytesIO(body), mode='r:gz') as archive:
        for filename in sorted(filenames):
            member = archive.getmember('package/' + filename)
            if not member.isfile():
                raise ValueError('Notice material is not a regular archive member')
            with archive.extractfile(member) as stream:
                if stream.read() != (Path(directory) / filename).read_bytes():
                    raise ValueError(f'Installed notice material differs from locked npm archive: {filename}')


def render(title, components):
    text = title + '\n\nOriginal upstream attribution material follows verbatim. The inventory records scope and provenance.\n'
    inventory = []
    for component in components:
        text += '\n' + '=' * 80 + '\n' + component['name'] + ' ' + component['version'] + '\nSource: ' + component['source'] + '\n'
        summary = {key: value for key, value in component.items() if key != 'notices'}
        summary['noticeSources'] = []
        for notice in component['notices']:
            text += '\n--- ' + notice['path'] + ' ---\n' + notice['text'] + '\n'
            summary['noticeSources'].append({key: value for key, value in notice.items() if key != 'text'})
        inventory.append(summary)
    return text.encode(), inventory


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', default='go', help='Exact release Go toolchain executable')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    go, packages = go_components(args.go)
    frontend, excluded = frontend_components()
    go_text, go_inventory = render('Engelsiz application Go attribution notices', go)
    frontend_text, frontend_inventory = render('Engelsiz application frontend attribution notices', frontend)
    files = {'go.txt': go_text, 'frontend.txt': frontend_text}
    inventory = {'format': 1, 'scope': 'Application only; separate Kiwix/runtime compliance is not asserted',
                 'goModuleCacheVerification': 'go mod verify passed before collection',
                 'goBuildSettings': {'GOARCH': 'amd64', 'GOAMD64': 'v1', 'CGO_ENABLED': '0'},
                 'inputs': {name: sha((ROOT / name).read_bytes()) for name in ('go.mod', 'go.sum', 'frontend/package-lock.json', 'frontend/tailwind.config.js')},
                 'go': go_inventory, 'goPackages': packages, 'frontend': frontend_inventory,
                 'frontendBuildOnlyExcluded': excluded,
                 'outputs': {name: {'sha256': sha(body), 'bytes': len(body)} for name, body in files.items()}}
    files['inventory.json'] = (json.dumps(inventory, ensure_ascii=False, indent=2, sort_keys=True) + '\n').encode()
    if args.check:
        stale = [name for name, body in files.items() if not (OUTPUT / name).is_file() or (OUTPUT / name).read_bytes() != body]
        if stale:
            raise SystemExit('Application attribution material is stale: ' + ', '.join(stale))
    else:
        OUTPUT.mkdir(parents=True, exist_ok=True)
        for name, body in files.items():
            (OUTPUT / name).write_bytes(body)
    print(json.dumps(inventory['outputs'], sort_keys=True))


if __name__ == '__main__':
    main()
