#!/usr/bin/env bash
# Run only inside the disposable pinned Ubuntu container from the outer driver.
set -euo pipefail
if [[ ! -f /run/.containerenv || ! -f /recipe.tar.gz || ! -d /work/ARCHIVE ]]; then
  echo 'This driver requires the disposable Podman container and verified source mounts' >&2
  exit 2
fi
export DEBIAN_FRONTEND=noninteractive
mkdir -p /work/provenance
cat /etc/os-release > /work/provenance/os-release
printf '%s\n' 'docker.io/library/ubuntu@sha256:076e7bf82319619f28dd0aa9c02ab34b055ad37100395b4f49a2ffba04fb5e11' > /work/provenance/container-image.txt
apt-get update
apt-get install --no-install-recommends -y build-essential ca-certificates git \
  python3-venv python3-pip cmake ninja-build pkg-config autoconf automake \
  libtool autopoint gettext texinfo file curl xz-utils
dpkg-query -W -f='${Package}\t${Version}\t${Architecture}\t${source:Package}\t${source:Version}\n' > /work/provenance/dpkg-packages.tsv
cp /etc/apt/sources.list /work/provenance/apt-sources.list
g++ --version > /work/provenance/compiler.txt
ld --version > /work/provenance/linker.txt
for runtime_build_file in libc.a libm.a libpthread.a libdl.a libstdc++.a libgcc.a libgcc_eh.a; do
  runtime_build_path=$(g++ -print-file-name="$runtime_build_file")
  if [[ -f "$runtime_build_path" ]]; then
    sha256sum "$runtime_build_path" >> /work/provenance/host-static-archives.sha256
    dpkg-query -S "$(realpath "$runtime_build_path")" >> /work/provenance/host-static-archive-owners.txt || true
  fi
done
readelf -n /lib/x86_64-linux-gnu/libc.so.6 > /work/provenance/libc-notes.txt
mkdir -p /work/provenance/host-notices
for runtime_build_package in libc6 libc6-dev libgcc-11-dev libstdc++-11-dev; do
  cp -L "/usr/share/doc/$runtime_build_package/copyright" "/work/provenance/host-notices/$runtime_build_package-copyright"
done
# Preserve original distro source archives and Debian patch/build metadata for
# incorporated host C/C++ runtimes. This does not declare license completeness.
sed -n 's/^deb /deb-src /p' /etc/apt/sources.list > /etc/apt/sources.list.d/engelsiz-source.list
apt-get update
mkdir -p /work/provenance/host-sources
cd /work/provenance/host-sources
apt-get source --download-only glibc gcc-11
sha256sum ./* > /work/provenance/host-source-archives.sha256
mkdir -p /work/recipe
tar -xzf /recipe.tar.gz -C /work/recipe --strip-components=1
python3 -m venv /work/build-venv
/work/build-venv/bin/pip install 'meson==1.9.2' 'setuptools==80.9.0' 'distro==1.9.0' 'paramiko==4.0.0'
/work/build-venv/bin/pip install --no-deps /work/recipe
/work/build-venv/bin/pip freeze > /work/provenance/python-build-requirements.txt
export PATH="/work/build-venv/bin:$PATH"
export KBUILD_NINJA_COMMAND='ninja -j4'
export CFLAGS='-O2 -march=x86-64 -mtune=generic'
export CXXFLAGS='-O2 -march=x86-64 -mtune=generic'
printf '%s\n' "$CFLAGS" > /work/provenance/vendor-cflags.txt
cd /work
kiwix-build kiwix-tools --config native_static --make-release --fast-clone \
  --dont-install-packages --working-dir /work --hide-progress
