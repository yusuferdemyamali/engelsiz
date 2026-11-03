#!/usr/bin/env python3
"""Generate attribution for the exact, pinned upstream PMTiles executables."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('notices', ROOT / 'scripts/collect-app-notices.py')
notices = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notices)
spec = importlib.util.spec_from_file_location('release', ROOT / 'scripts/release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


def run(go, args, cwd, env=None):
    return subprocess.check_output([go, *args], cwd=cwd, env={**os.environ, 'GOTOOLCHAIN': 'local', 'GOWORK': 'off', **(env or {})}, text=True, encoding='utf-8')


def collect(go, archives):
    locks = json.loads((ROOT / 'third_party/pmtiles-lock.json').read_text())
    goroot = Path(run(go, ['env', 'GOROOT'], ROOT).strip())
    goversion = run(go, ['env', 'GOVERSION'], ROOT).strip()
    if goversion != 'go1.26.1':
        raise ValueError('PMTiles upstream binary was built with go1.26.1; use that exact Go toolchain')
    module_info = json.loads(run(go, ['mod', 'download', '-json', 'github.com/protomaps/go-pmtiles@v1.31.2'], ROOT))
    if module_info.get('Sum') != 'h1:xYXagv2oKJarheCdOIGg8MvYRp8Y03MBs9jegxf4NDQ=' or module_info.get('Origin', {}).get('Hash') != 'a3e4951ea6a0477b784c27c1dcbfd9c130878c5a':
        raise ValueError('Unexpected PMTiles source module')
    source = Path(module_info['Dir'])
    run(go, ['mod', 'verify'], source)
    packages, modules, platform_imports = {}, {}, {}
    for platform, archive in sorted(archives.items()):
        lock = locks[platform]
        release.checked(archive, lock['sha256'])
        import tempfile
        with tempfile.TemporaryDirectory() as temp:
            binary = release.extract_runtime(archive, lock, temp)
            buildinfo = run(go, ['version', '-m', str(binary)], ROOT)
        if 'go1.26.1' not in buildinfo or '\tvcs.revision=a3e4951ea6a0477b784c27c1dcbfd9c130878c5a' not in buildinfo:
            raise ValueError('Unexpected upstream binary build information')
        expected = {}
        for line in buildinfo.splitlines():
            parts = line.split()
            if parts and parts[0] == 'dep':
                if len(parts) != 4:
                    raise ValueError('Replaced or malformed binary module')
                expected[parts[1]] = (parts[2], parts[3])
        osname = platform.split('-')[0]
        env = {'GOOS': osname, 'GOARCH': 'amd64', 'GOAMD64': 'v1', 'CGO_ENABLED': '0'}
        selected = list(notices.json_stream(run(go, ['list', '-mod=readonly', '-deps', '-json', '.'], source, env)))
        selected_modules = {}
        platform_imports[platform] = sorted(p['ImportPath'] for p in selected)
        for package in selected:
            module = package.get('Module')
            if module and not module.get('Main'):
                if module.get('Replace'):
                    raise ValueError('Replaced source dependency')
                selected_modules[module['Path']] = (module['Version'], module.get('Sum'))
                modules[module['Path']] = module
            packages[(platform, package['ImportPath'])] = package
        if selected_modules != expected:
            raise ValueError(f'{platform} source graph does not match upstream binary module graph')
    # Verify all modules after list has populated the cache.
    run(go, ['mod', 'verify'], source)
    components = [{'name': 'PMTiles', 'version': 'v1.31.2', 'source': 'https://github.com/protomaps/go-pmtiles/tree/v1.31.2', 'notices': notices.root_notices(source)},
                  {'name': 'Go runtime and standard library', 'version': goversion, 'source': 'https://github.com/golang/go/tree/go1.26.1', 'notices': notices.root_notices(goroot)}]
    vendors = sorted({'/'.join(p['ImportPath'].split('/')[:4]) for p in packages.values() if p['ImportPath'].startswith('vendor/golang.org/x/')})
    for vendor in vendors:
        components.append({'name': vendor.removeprefix('vendor/'), 'version': goversion, 'source': f'https://github.com/golang/go/tree/{goversion}/src/{vendor}', 'notices': notices.root_notices(goroot / 'src' / vendor)})
    for name, module in sorted(modules.items()):
        components.append({'name': name, 'version': module['Version'], 'moduleSum': module['Sum'], 'source': f'https://proxy.golang.org/{name}/@v/{module["Version"]}.zip', 'notices': notices.root_notices(module['Dir'])})
    blocks = {}
    for package in packages.values():
        module = package.get('Module')
        directory = Path(package['Dir'])
        base = Path(module['Dir']) if module else goroot
        label = f'{module["Path"]}@{module.get("Version", "v1.31.2")}' if module else goversion
        for filename in package.get('GoFiles', []) + package.get('SFiles', []):
            path = directory / filename
            body = path.read_bytes()
            for match in notices.COMMENT.finditer(body.decode('utf-8')):
                block = match.group(0)
                if 'copyright' not in block.lower():
                    continue
                key = notices.sha(block.encode())
                entry = blocks.setdefault(key, {'path': 'inline copyright block ' + key, 'sha256': key, 'text': block, 'origins': []})
                origin = {'source': f'{label}/{path.relative_to(base).as_posix()}', 'sourceSha256': notices.sha(body)}
                if origin not in entry['origins']:
                    entry['origins'].append(origin)
    for entry in blocks.values():
        entry['origins'].sort(key=lambda origin: origin['source'])
    components.append({'name': 'Inline notices from selected Go source/assembly', 'version': goversion, 'source': 'Selected go list -deps files for linux-amd64 and windows-amd64', 'notices': [blocks[key] for key in sorted(blocks)]})
    body, entries = notices.render('PMTiles 1.31.2 dependency attribution', components)
    inventory = {'format': 1, 'archiveSha256': {p: locks[p]['sha256'] for p in sorted(archives)}, 'sourceModuleSum': module_info['Sum'], 'goVersion': goversion, 'goModVerify': True, 'binaryModuleGraphMatchesSource': True, 'components': entries, 'packages': platform_imports, 'noticeSha256': notices.sha(body)}
    return body, (json.dumps(inventory, ensure_ascii=False, sort_keys=True, indent=2) + '\n').encode()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', required=True)
    parser.add_argument('--linux-archive', type=Path, required=True)
    parser.add_argument('--windows-archive', type=Path, required=True)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    body, inventory = collect(args.go, {'linux-amd64': args.linux_archive, 'windows-amd64': args.windows_archive})
    directory = ROOT / 'third_party/pmtiles-notices'
    directory.mkdir(parents=True, exist_ok=True)
    for name, expected in [('notices.txt', body), ('inventory.json', inventory)]:
        target = directory / name
        if args.check:
            if not target.is_file() or target.read_bytes() != expected:
                raise ValueError(f'Stale PMTiles notices: {name}')
        else:
            target.write_bytes(expected)
        print(target)


if __name__ == '__main__':
    main()
