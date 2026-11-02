#!/usr/bin/env bash
# Maintainer-only MinGW source build; requires the captured /work inputs.
# Run inside a disposable Ubuntu 22.04 container, never directly on the host.
set -euo pipefail
[[ -f /run/.containerenv && -d /work/known-source/SOURCE ]] || exit 2
# The preferred-source snapshot must match the audit's seed hash before extraction.
# Apply this Windows-only patch to a pristine kiwix-tools 3.8.2 source tree.
patch_file=/work/windows-patches/0001-unicode-command-line.patch
if ! grep -q 'int wmain' /work/known-source/SOURCE/kiwix-tools_release/src/server/kiwix-serve.cpp; then
  patch -d /work/known-source/SOURCE/kiwix-tools_release -p1 < "$patch_file"
fi
if ! grep -q 'countRaw' /work/known-source/SOURCE/libkiwix_release/src/search_renderer.cpp; then
  patch -d /work/known-source/SOURCE/libkiwix_release -p1 < /work/windows-patches/0002-opensearch-integer-fields.patch
fi
export DEBIAN_FRONTEND=noninteractive
S=/work/known-source/SOURCE
B=/work/cross-build
P=/work/cross-prefix
mkdir -p "$B" "$P" /work/provenance
python3 -m venv /work/cross-venv
/work/cross-venv/bin/pip install 'meson==1.9.2' > /work/logs/cross-pip.log 2>&1
export PATH=/work/cross-venv/bin:$PATH
export PKG_CONFIG_LIBDIR="$P/lib/pkgconfig"
export PKG_CONFIG_PATH="$P/lib/pkgconfig"
export CFLAGS='-O2 -march=x86-64 -mtune=generic'
export CXXFLAGS="$CFLAGS"
cat > /work/cross.ini <<EOF
[binaries]
c = 'x86_64-w64-mingw32-gcc'
cpp = 'x86_64-w64-mingw32-g++'
ar = 'x86_64-w64-mingw32-ar'
strip = 'x86_64-w64-mingw32-strip'
windres = 'x86_64-w64-mingw32-windres'
pkg-config = 'pkg-config'
[host_machine]
system = 'windows'
cpu_family = 'x86_64'
cpu = 'x86_64'
endian = 'little'
[properties]
needs_exe_wrapper = true
[built-in options]
c_args = ['-O2', '-march=x86-64', '-mtune=generic', '-D_WIN32_WINNT=0x0a00', '-I$P/include']
cpp_args = ['-O2', '-march=x86-64', '-mtune=generic', '-D_WIN32_WINNT=0x0a00', '-I$P/include']
c_link_args = ['-Wl,--no-insert-timestamp', '-static', '-L$P/lib']
cpp_link_args = ['-Wl,--no-insert-timestamp', '-static', '-L$P/lib', '-lws2_32', '-lrpcrt4', '-lwinmm', '-lshlwapi', '-liphlpapi']
EOF
step() { local name=$1; shift; echo "START $name"; "$@" > "/work/logs/cross-$name.log" 2>&1; echo "OK $name"; }
meson_build() {
 local name=$1 source=$2; shift 2
 if [[ ! -f "$B/$name/build.ninja" ]]; then
 meson setup "$B/$name" "$source" --cross-file /work/cross.ini --prefix "$P" --libdir lib --buildtype release --default-library static --wrap-mode nodownload -Dauto_features=disabled "$@"
 fi
 ninja -C "$B/$name" -j4
 ninja -C "$B/$name" install
}
icu_build() {
 mkdir -p "$B/icu-native" "$B/icu-cross"
 cd "$B/icu-native"
 if [[ ! -f Makefile ]]; then "$S/icu4c-73.2/source/configure" --prefix=/work/icu-native-prefix --disable-shared --enable-static --disable-tests --disable-samples --disable-extras --disable-icuio --disable-dyload; fi
 make -j4
 cd "$B/icu-cross"
 if [[ ! -f Makefile ]]; then CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ AR=x86_64-w64-mingw32-ar RANLIB=x86_64-w64-mingw32-ranlib "$S/icu4c-73.2/source/configure" --host=x86_64-w64-mingw32 --with-cross-build="$B/icu-native" --prefix="$P" --disable-shared --enable-static --disable-tests --disable-samples --disable-extras --disable-icuio --disable-dyload; fi
 make -j4
 make install
}
step icu icu_build &
icu_pid=$!
trap 'if [[ -n "${icu_pid:-}" ]]; then kill "$icu_pid" 2>/dev/null || true; fi' EXIT
step zlib meson_build zlib "$S/zlib-1.3.1" -Dtests=disabled
step zstd meson_build zstd "$S/zstd-1.5.7/build/meson" -Dbin_programs=false -Dbin_contrib=false
step lzma bash -ec 'mkdir -p /work/cross-build/lzma; cd /work/cross-build/lzma; /work/known-source/SOURCE/lzma-5.2.6/configure --host=x86_64-w64-mingw32 --prefix=/work/cross-prefix --disable-shared --enable-static --disable-xz --disable-xzdec --disable-lzmadec --disable-lzmainfo --disable-scripts --disable-doc; make -j4; make install'
step xapian bash -ec 'mkdir -p /work/cross-build/xapian; cd /work/cross-build/xapian; CPPFLAGS=-I/work/cross-prefix/include LDFLAGS=-L/work/cross-prefix/lib /work/known-source/SOURCE/xapian-core-1.4.26/configure --host=x86_64-w64-mingw32 --prefix=/work/cross-prefix --disable-shared --enable-static --disable-sse --disable-backend-chert --disable-backend-remote --disable-documentation; make -j4; make install'
step docopt meson_build docopt "$S/docoptcpp-0.6.2"
step pugixml meson_build pugixml "$S/pugixml-1.15"
cp "$S/mustache-4.1/mustache.hpp" "$P/include/"
step microhttpd meson_build microhttpd "$S/libmicrohttpd-0.9.76" -Dgnutls=disabled -Dgcrypt=disabled -Dcurl=disabled -Dexpat=disabled
step curl meson_build curl "$S/libcurl" -Dlibz=enabled -Dhttp=enabled -Dipv6=enabled
wait "$icu_pid"
unset icu_pid
# Replace ICU's stub with its actual cross-compiled static data archive.
cp "$B/icu-cross/lib/icudt.a" "$P/lib/libicudt.a"
step libzim meson_build libzim "$S/libzim_release" -Dstatic-linkage=true -Dtests=false -Dexamples=false -Dtest_data_dir=none -Dwerror=false
step libkiwix meson_build libkiwix "$S/libkiwix_release" -Dstatic-linkage=true -Dwerror=false
step kiwix-tools meson_build kiwix-tools "$S/kiwix-tools_release" -Dstatic-linkage=true -Dwerror=false
x86_64-w64-mingw32-g++ --version > /work/provenance/cross-compiler.txt
dpkg-query -W -f='${Package}\t${Version}\t${source:Package}\t${source:Version}\n' > /work/provenance/cross-dpkg.tsv
sha256sum "$P"/bin/*.exe > /work/provenance/cross-executables.sha256
