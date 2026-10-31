#!/usr/bin/env bash
# Candidate runtime research build; does not update runtime-lock.json or publish.
set -euo pipefail
if [[ $# != 2 ]]; then
  echo "Usage: bash $0 WORK_DIRECTORY KIWIX_BUILD_R100_ARCHIVE" >&2
  exit 2
fi
runtime_build_work=$(realpath "$1")
runtime_build_recipe=$(realpath "$2")
runtime_build_scripts=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
runtime_build_image='docker.io/library/ubuntu@sha256:076e7bf82319619f28dd0aa9c02ab34b055ad37100395b4f49a2ffba04fb5e11'
if [[ "$runtime_build_work$runtime_build_recipe$runtime_build_scripts" == *,* ]]; then
  echo 'Comma characters in Podman mount paths are unsupported' >&2
  exit 2
fi
python3 - "$runtime_build_work" "$runtime_build_recipe" "$runtime_build_scripts/linux-r100.json" <<'PY'
import hashlib, json, pathlib, sys
work, recipe, inventory = map(pathlib.Path, sys.argv[1:])
data = json.loads(inventory.read_text())
if (work / 'BUILD_native_static').exists():
    raise SystemExit('Refusing to reuse compiler objects from a previous build')
if hashlib.file_digest(recipe.open('rb'), 'sha256').hexdigest() != data['buildRecipe']['sha256']:
    raise SystemExit('Build recipe archive SHA-256 mismatch')
for item in data['records']:
    if item.get('recipe'):
        path = work / 'ARCHIVE' / item['name']
        if not path.is_file() or hashlib.file_digest(path.open('rb'), 'sha256').hexdigest() != item['sha256']:
            raise SystemExit('Missing or changed source input: ' + item['name'])
PY
podman run --rm --name engelsiz-runtime-source-build \
  --mount "type=bind,source=$runtime_build_work,destination=/work" \
  --mount "type=bind,source=$runtime_build_recipe,destination=/recipe.tar.gz,ro=true" \
  --mount "type=bind,source=$runtime_build_scripts,destination=/build-script,ro=true" \
  "$runtime_build_image" bash -c \
  'cp /build-script/build-linux-container-inner.sh /tmp/engelsiz-container-driver.sh; exec bash /tmp/engelsiz-container-driver.sh'
