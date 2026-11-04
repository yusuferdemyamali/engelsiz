#!/usr/bin/env python3
"""Build the frozen offline translation runtime (PyInstaller onedir).

Produces a native directory (no Python/pip needed at runtime) containing the
`engelsiz-translate` worker binary, its THIRD-PARTY-NOTICES, and a
translation-runtime-manifest.json with hash pins, dependency versions, and
the audited Linux shared-library allowlist.

Usage:
  python scripts/translation/build_runtime.py [--output DIR] [--python PY]
      [--find-links DIR]... [--no-index] [--skip-smoke]

Defaults: --output <repository>/dist/translation-runtime,
--python python3.11 (must be CPython 3.11.x; must match requirements pins).

Fails closed on: wrong interpreter, forbidden heavy packages present
(torch/stanza/spacy/minisbd/sacremoses/nvidia/cuda), PyInstaller errors, or
the post-build smoke test (no models required).
"""

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
WORKER = HERE / "worker" / "engelsiz_translate.py"
RUNTIME_REQUIREMENTS = HERE / "requirements-runtime.txt"
BUILD_REQUIREMENTS = HERE / "requirements-build.txt"

FORBIDDEN_PREFIXES = (
    "torch",
    "stanza",
    "spacy",
    "minisbd",
    "sacremoses",
    "nvidia",
    "cuda",
    "triton",
)

RUNTIME_PACKAGES = (
    "argostranslate",
    "ctranslate2",
    "sentencepiece",
    "numpy",
    "pyyaml",
    "packaging",
)


def run(*args, **kwargs):
    subprocess.run(args, check=True, **kwargs)


def interpreter(python):
    candidates = []
    if python:
        candidates.append(python)
    candidates.extend(["python3.11", sys.executable])
    for candidate in candidates:
        path = shutil.which(candidate) or candidate
        try:
            result = subprocess.run(
                [str(path), "-c",
                 "import sys; print(sys.version.split()[0], sys.version_info.releaselevel)"],
                check=True,
                capture_output=True,
                text=True,
            )
        except (OSError, subprocess.CalledProcessError):
            continue
        version, _, level = result.stdout.strip().partition(" ")
        # Prerelease interpreters (alpha/beta/rc) are rejected: the frozen
        # runtime must be built with a stable security release.
        if version.startswith("3.11.") and level == "final":
            return str(path)
    raise SystemExit(
        "Release blocked: stable CPython 3.11.x final interpreter is required "
        "(prereleases rejected; pass --python /path/to/python3.11)"
    )


def freeze(python):
    result = subprocess.run(
        [python, "-m", "pip", "freeze", "--all"],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout


def check_runtime_env(python):
    frozen = freeze(python)
    installed = {}
    for line in frozen.splitlines():
        if "==" in line:
            name, _, version = line.partition("==")
            installed[name.strip().lower()] = version.strip()
    for name in RUNTIME_PACKAGES:
        if name not in installed:
            raise SystemExit(f"Release blocked: runtime package missing: {name}")
    offenders = sorted(
        name for name in installed if name.startswith(FORBIDDEN_PREFIXES)
    )
    if offenders:
        raise SystemExit(
            "Release blocked: forbidden heavy packages in runtime env: "
            + ", ".join(offenders)
        )
    # Platform markers in requirements-build.txt select the Windows-only
    # closure (pefile, pywin32-ctypes); old pip releases mishandle
    # markers combined with --hash, so enforce the selection here too.
    if os.name == "nt":
        for name in ("pefile", "pywin32-ctypes"):
            if name not in installed:
                raise SystemExit(f"Release blocked: Windows build package missing: {name}")
    else:
        unexpected = sorted(
            name for name in ("pefile", "pywin32-ctypes", "macholib") if name in installed
        )
        if unexpected:
            raise SystemExit(
                "Release blocked: Windows-only build packages on Linux: "
                + ", ".join(unexpected)
            )
    return installed


def wheel_hashes(python):
    """Map installed runtime dist -> wheel filename + sha256 (from installer)."""
    code = (
        "import importlib.metadata as m, json;"
        "print(json.dumps({d.metadata['Name']: (getattr(d, '_path', None) and str(d._path)) for d in m.distributions()}))"
    )
    _ = code
    # installer records are not reliably present; instead hash the pinned
    # requirement lines (verified by --require-hashes at install time) and
    # record installed versions from pip freeze.
    return {}


def license_notice(env_site, dist_name, destination):
    matches = sorted(env_site.glob(dist_name.replace("-", "_") + "-*.dist-info")) + sorted(
        env_site.glob(dist_name.replace("_", "-") + "-*.dist-info")
    )
    if not matches:
        # Fall back to case-insensitive scan.
        lowered = dist_name.replace("-", "_").lower()
        matches = [
            p
            for p in env_site.glob("*.dist-info")
            if p.name.replace("-", "_").lower().startswith(lowered + "-")
        ]
    if not matches:
        raise SystemExit(f"Release blocked: dist-info missing for {dist_name}")
    info = matches[0]
    target_dir = destination / info.name
    target_dir.mkdir(parents=True, exist_ok=True)
    for child in sorted(info.iterdir()):
        if child.is_file() and (
            child.suffix.lower() in (".txt", ".md", ".rst", "")
            or child.name.upper().startswith(
                ("LICENSE", "LICENCE", "COPYING", "NOTICE", "METADATA", "WHEEL")
            )
        ):
            shutil.copy2(child, target_dir / child.name)
    # Guarantee at least the METADATA attribution is present.
    if not (target_dir / "METADATA").is_file() and (info / "METADATA").is_file():
        shutil.copy2(info / "METADATA", target_dir / "METADATA")
    # Modern installers place METADATA License-File targets under
    # dist-info/licenses/; preserve that tree verbatim.
    if (info / "licenses").is_dir():
        shutil.copytree(info / "licenses", target_dir / "licenses",
                        symlinks=False)
    # Resolve METADATA License-File entries (wheels declaring license texts
    # shipped inside the package, e.g. numpy's bundled third-party licenses).
    metadata_text = (info / "METADATA").read_text(encoding="utf-8", errors="replace")
    license_files = [
        line.split(":", 1)[1].strip()
        for line in metadata_text.splitlines()
        if line.startswith("License-File:")
    ]
    copied_license_text = any(
        (target_dir / name).is_file()
        for name in ("LICENSE", "LICENCE", "COPYING", "NOTICE")
    ) or any(
        child.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE"))
        for child in target_dir.iterdir()
    )
    for pattern in license_files:
        for source in sorted(env_site.glob(pattern)):
            if not source.is_file() or source.is_symlink():
                continue
            relative = source.relative_to(env_site)
            target = target_dir / ("_license-files") / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
            copied_license_text = True
    if not copied_license_text:
        # The wheel ships no license text; record the declared SPDX/license
        # plus the upstream home so the notice stays attributable.
        fields = {}
        for line in metadata_text.splitlines():
            if line.startswith(("Name:", "Version:", "License:", "License-Expression:", "Home-page:")):
                key, _, value = line.partition(":")
                fields.setdefault(key.strip(), value.strip())
        (target_dir / "LICENSE-REFERENCE.txt").write_text(
            "Wheel package license reference (no license text ships in the wheel).\n"
            + "\n".join(f"{key}: {value}" for key, value in fields.items())
            + "\n",
            encoding="utf-8",
        )
    return info.name


def site_packages(venv):
    if os.name == "nt":
        return Path(venv) / "Lib" / "site-packages"
    return Path(venv) / "lib" / "python3.11" / "site-packages"


def dpkg_copyright(package):
    """Read a Debian copyright file (native-library license provenance)."""
    candidate = Path("/usr/share/doc") / package / "copyright"
    if candidate.is_file() and not candidate.is_symlink():
        return candidate.read_text(encoding="utf-8", errors="replace")
    return None


def dpkg_version(package):
    try:
        result = subprocess.run(
            ["dpkg-query", "-W", "-f=${Version}", package],
            capture_output=True,
            text=True,
            check=False,
        )
    except OSError:
        return None
    return result.stdout.strip() or None


# Bundled native libraries collected from the build host system (not wheels).
# Maps a basename regex to the Debian source package whose copyright file
# documents the license. Versions come from dpkg at build time.
SYSTEM_LIB_ORIGINS = (
    (r"libz\.so.*", "zlib1g"),
    (r"libstdc\+\+\.so.*", "libstdc++6"),
    (r"libgcc_s\.so.*", "libgcc-s1"),
    (r"libgfortran.*\.so.*", "libgfortran5"),
    (r"libquadmath.*\.so.*", "libquadmath0"),
    (r"libgomp.*\.so.*", "libgomp1"),
    (r"libssl\.so\..*", "libssl3"),
    (r"libcrypto\.so\..*", "libssl3"),
    (r"libbz2\.so.*", "libbz2-1.0"),
    (r"liblzma\.so.*", "liblzma5"),
    (r"libexpat\.so.*", "libexpat1"),
    (r"libffi\.so.*", "libffi8"),
    (r"libmpdec\.so.*", "libmpdec3"),
    (r"libtinfo\.so.*", "libtinfo6"),
    (r"libreadline\.so.*", "libreadline8"),
    (r"libsqlite3\.so.*", "libsqlite3-0"),
)

# Redistributables are never silently bundled without papers.
REDIST_PATTERNS = (r"vcruntime.*\.dll", r"msvcp.*\.dll", r"vcomp.*\.dll")

API_SET_PATTERNS = (r"api-ms-win-.*", r"ext-ms-win-.*")


def find_interpreter_license(venv_python, override=None):
    """Locate the CPython PSF license text for the build interpreter.

    Fail closed: no bare-URL or generated substitute is accepted.
    """
    if override:
        candidate = Path(override)
        if candidate.is_file():
            text = candidate.read_text(encoding="utf-8", errors="replace")
            if "PYTHON SOFTWARE FOUNDATION LICENSE" in text:
                return text
        raise SystemExit(
            "Release blocked: --python-license does not contain the PSF license text"
        )
    code = (
        "import sys,sysconfig,pathlib;"
        "roots={pathlib.Path(sys.base_prefix),pathlib.Path(sysconfig.get_path('data')),pathlib.Path(sys.executable).parent.parent};"
        "print('\\n'.join(str(r) for r in roots))"
    )
    result = subprocess.run(
        [venv_python, "-c", code], check=True, capture_output=True, text=True
    )
    for root in dict.fromkeys(result.stdout.splitlines()):
        for name in ("LICENSE", "LICENSE.txt"):
            candidate = Path(root.strip()) / name
            if candidate.is_file() and not candidate.is_symlink():
                text = candidate.read_text(encoding="utf-8", errors="replace")
                if "PYTHON SOFTWARE FOUNDATION LICENSE" in text:
                    return text
    # Debian/Ubuntu system interpreters document the PSF grant in their
    # copyright files instead of a LICENSE file; same marker required.
    for candidate in sorted(Path("/usr/share/doc").glob("python3.11*/copyright")) + sorted(
        Path("/usr/share/doc").glob("libpython3.11*/copyright")
    ):
        if candidate.is_file() and not candidate.is_symlink():
            text = candidate.read_text(encoding="utf-8", errors="replace")
            if "PYTHON SOFTWARE FOUNDATION LICENSE" in text:
                return text
    raise SystemExit(
        "Release blocked: CPython PSF license text not found for the build "
        "interpreter; pass --python-license /path/to/LICENSE"
    )


def record_index(env_site):
    """Index installed runtime wheel files by basename from RECORD files.

    Returns {basename: [(dist_name, version, sha256_hex)]} for license
    attribution of frozen native libraries back to their source wheel.
    RECORD hashes are base64url without padding; normalized to hex.
    """
    import base64
    import csv

    def record_hex(digest_field):
        if "=" not in digest_field:
            return ""
        encoded = digest_field.split("=", 1)[1].strip()
        if not encoded:
            return ""
        try:
            return base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4)).hex()
        except (ValueError, base64.binascii.Error):
            return ""

    index = {}
    for dist in Path(env_site).glob("*.dist-info"):
        metadata = dist / "METADATA"
        name = version = None
        if metadata.is_file():
            for line in metadata.read_text(encoding="utf-8", errors="replace").splitlines():
                if line.startswith("Name:"):
                    name = line.partition(":")[2].strip()
                elif line.startswith("Version:"):
                    version = line.partition(":")[2].strip()
                if name and version:
                    break
        record = dist / "RECORD"
        if not record.is_file():
            continue
        with record.open("r", encoding="utf-8", errors="replace", newline="") as stream:
            for row in csv.reader(stream):
                if not row:
                    continue
                rel, digest_field = row[0], (row[1] if len(row) > 1 else "")
                digest_value = record_hex(digest_field)
                index.setdefault(Path(rel).name, []).append((name, version, digest_value))
    return index


def collect_native_notices(frozen, binary_name, env_site, notices, venv_python,
                         python_license=None, native_overrides=()):
    """Attribute every shipped native library with origin/version/license.

    Writes THIRD-PARTY-NOTICES/NATIVE-LIBS.json plus copied license texts.
    Fail closed on any unattributed binary: unknown origin, missing Debian
    copyright, missing interpreter LICENSE, or a redistributable DLL
    without explicitly supplied papers. A bare URL is never accepted as a
    substitute for required license text.
    """
    entries = {}
    index = record_index(env_site)
    native_dir = notices / "native"
    native_dir.mkdir(parents=True, exist_ok=True)
    override_map = {}
    for item in native_overrides:
        key, _, path = item.partition("=")
        if not key or not path:
            raise SystemExit("Release blocked: --native-license needs NAME=PATH")
        override_map[key.lower()] = Path(path)
    for path in sorted(frozen.rglob("*")):
        if not path.is_file() or path.is_symlink():
            continue
        if path.name == binary_name:
            continue
        if not re.fullmatch(r".*\.so(\.[0-9]+)*", path.name) and path.suffix.lower() not in (".dll", ".pyd"):
            continue
        lowered = path.name.lower()
        digest_here = sha256_of(path)
        if any(re.fullmatch(pattern, lowered) for pattern in API_SET_PATTERNS):
            raise SystemExit(
                f"Release blocked: API-set library must stay host-provided, not bundled: {path.name}"
            )
        if any(re.fullmatch(pattern, lowered) for pattern in REDIST_PATTERNS):
            override = override_map.get(lowered)
            if override is None or not override.is_file():
                raise SystemExit(
                    f"Release blocked: redistributable {path.name} needs explicit "
                    f"license papers via --native-license {path.name}=PATH"
                )
            target = native_dir / (path.name + ".license.txt")
            shutil.copy2(override, target)
            entries[path.relative_to(frozen).as_posix()] = {
                "origin": "redistributable bundled by PyInstaller (maintainer-supplied papers)",
                "license": str(target.relative_to(notices)),
            }
            continue
        if path.name.startswith("libpython3") or path.name == "python311.dll":
            entries[path.relative_to(frozen).as_posix()] = {
                "origin": "CPython interpreter",
                "license": "interpreter-LICENSE.txt",
            }
            continue
        candidates = index.get(path.name, [])
        matched = [c for c in candidates if c[2] in ("", digest_here)]
        if matched and any(c[2] == digest_here for c in matched):
            dist_name, version, _ = next(c for c in matched if c[2] == digest_here)
            entries[path.relative_to(frozen).as_posix()] = {
                "origin": f"wheel {dist_name}=={version}",
                "license": f"{dist_name}-attribution",
            }
            continue
        if matched and sys.platform == "win32":
            # Hash-less RECORD rows (pip legacy): attribute by name only on
            # Windows wheels where RECORD omits hashes.
            dist_name, version, _ = matched[0]
            entries[path.relative_to(frozen).as_posix()] = {
                "origin": f"wheel {dist_name}=={version}",
                "license": f"{dist_name}-attribution",
            }
            continue
        # CPython stdlib extensions (lib-dynload) carry no wheel RECORD and
        # are covered by the interpreter PSF license, same as libpython.
        # Only when no wheel claims the basename: a hash-mismatched wheel
        # file must keep falling through to fail closed, never be
        # misattributed as stdlib.
        if not candidates and ("cpython-311" in path.name or ".cp311-" in path.name):
            entries[path.relative_to(frozen).as_posix()] = {
                "origin": "CPython 3.11 stdlib",
                "license": "interpreter-LICENSE.txt",
            }
            continue
        system_package = None
        if sys.platform == "linux":
            for pattern, package in SYSTEM_LIB_ORIGINS:
                if re.fullmatch(pattern, path.name):
                    system_package = package
                    break
        if system_package is None:
            raise SystemExit(
                f"Release blocked: unattributed native library {path.name}; "
                f"supply papers via --native-license {path.name}=PATH"
            )
        copyright_text = dpkg_copyright(system_package)
        version = dpkg_version(system_package)
        if not copyright_text or not version:
            raise SystemExit(
                f"Release blocked: no Debian copyright/version for {path.name} "
                f"({system_package}); supply papers via --native-license"
            )
        target = native_dir / (path.name + ".copyright")
        target.write_text(copyright_text, encoding="utf-8")
        entries[path.relative_to(frozen).as_posix()] = {
            "origin": f"Debian {system_package}=={version} (host-collected)",
            "license": str(target.relative_to(notices)),
        }
    (notices / "interpreter-LICENSE.txt").write_text(
        find_interpreter_license(venv_python, python_license),
        encoding="utf-8",
    )
    (notices / "NATIVE-LIBS.json").write_text(
        json.dumps(entries, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    return entries


def desymlink_tree(root):
    """Replace internal symlinks with real copies (fail closed otherwise).

    PyInstaller deduplicates identical collected binaries as symlinks. The
    release policy forbids symlinks anywhere in the artifact, so every
    symlink whose target is a regular file inside the tree is materialized
    into an independent copy with identical bytes (same hash, deterministic
    inventory). Dangling links or links escaping the tree fail closed.
    """
    root = Path(root)
    for path in sorted(root.rglob("*")):
        if not path.is_symlink():
            continue
        try:
            target = path.resolve(strict=True)
        except OSError:
            raise SystemExit(f"Release blocked: dangling link in frozen runtime: {path.name}")
        if root not in target.parents or not target.is_file():
            raise SystemExit(f"Release blocked: link escapes frozen runtime: {path.name}")
        path.unlink()
        shutil.copyfile(target, path)


def file_inventory(frozen):
    """Per-file (size, sha256) inventory of the frozen runtime, sorted."""
    frozen = Path(frozen)
    entries = []
    for path in sorted(frozen.rglob("*")):
        if path.is_symlink() or (not path.is_file() and not path.is_dir()):
            raise SystemExit(f"Release blocked: unsafe member in frozen runtime: {path.name}")
        if path.is_file() and path.name != "translation-runtime-manifest.json":
            relative = path.relative_to(frozen).as_posix()
            entries.append(
                {"path": relative, "size": path.stat().st_size, "sha256": sha256_of(path)}
            )
    return entries
    if os.name == "nt":
        return Path(venv) / "Lib" / "site-packages"
    return Path(venv) / "lib" / "python3.11" / "site-packages"


def glibc_version():
    if os.name != "posix" or sys.platform != "linux":
        return None
    try:
        result = subprocess.run(
            ["ldd", "--version"], capture_output=True, text=True, check=False
        )
        first = (result.stdout or result.stderr or "").splitlines()
        return first[0].strip() if first else None
    except OSError:
        return None


def shared_library_allowlist(frozen_dir):
    return sorted(
        {
            path.name
            for path in Path(frozen_dir).rglob("*.so*")
            if path.is_file() and not path.is_symlink()
        }
    )


def sha256_of(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def requirement_pins(requirements_file):
    pins = {}
    name = None
    for line in Path(requirements_file).read_text(encoding="utf-8").splitlines():
        line = line.strip().rstrip("\\").strip()
        if not line or line.startswith("#") or line.startswith("--"):
            if line.startswith("--hash"):
                pins.setdefault(name, []).append(line.split(":", 1)[1])
            continue
        name = line.split("=")[0].strip().lower()
        pins.setdefault(name, [])
    return pins


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--output",
        type=Path,
        default=ROOT / "dist" / "translation-runtime",
    )
    parser.add_argument("--python", default=None)
    parser.add_argument("--find-links", action="append", default=[])
    parser.add_argument("--no-index", action="store_true")
    parser.add_argument("--skip-smoke", action="store_true")
    parser.add_argument("--python-license", default=None,
                        help="Explicit CPython LICENSE file if interpreter discovery fails")
    parser.add_argument("--native-license", action="append", default=[],
                        help="Explicit native-library papers as NAME=PATH (repeatable)")
    args = parser.parse_args()

    python = interpreter(args.python)
    output = args.output
    output.mkdir(parents=True, exist_ok=True)
    if sys.platform == 'linux' and shutil.which('objdump') is None:
        raise SystemExit(
            "Release blocked: PyInstaller requires objdump on Linux "
            "(install the 'binutils' package)"
        )

    with tempfile.TemporaryDirectory(prefix="engelsiz-translation-build-") as temporary:
        temporary = Path(temporary)
        venv = temporary / "venv"
        run(python, "-m", "venv", str(venv))
        venv_python = (
            str(venv / "Scripts" / "python.exe")
            if os.name == "nt"
            else str(venv / "bin" / "python")
        )
        # The build bootstrap itself is pinned: no unpinned `pip --upgrade`,
        # binary wheels only, hashes enforced, no transitive resolution.
        index_args = ["--only-binary", ":all:"]
        for link in args.find_links:
            index_args += ["--find-links", link]
        if args.no_index:
            index_args.append("--no-index")
        run(
            venv_python,
            "-m",
            "pip",
            "install",
            "--require-hashes",
            "--no-deps",
            *index_args,
            "-r",
            str(RUNTIME_REQUIREMENTS),
        )
        run(
            venv_python,
            "-m",
            "pip",
            "install",
            "--require-hashes",
            "--no-deps",
            *index_args,
            "-r",
            str(BUILD_REQUIREMENTS),
        )
        # Platform markers select the Windows-only closure, but old pip
        # releases install marker-mismatched packages when --hash is used;
        # evict them so the Linux build env stays exactly as pinned.
        if os.name != "nt":
            subprocess.run(
                [venv_python, "-m", "pip", "uninstall", "-y",
                 "pefile", "pywin32-ctypes", "macholib"],
                check=False,
                capture_output=True,
            )
        installed = check_runtime_env(venv_python)

        # Fail fast if the worker source cannot even import in the build env.
        run(
            venv_python,
            "-c",
            "import sys; sys.argv=['x','/nonexistent']; "
            "sys.path.insert(0, %r); "
            "import engelsiz_translate" % str(WORKER.parent),
            cwd=str(ROOT),
        )

        dist_path = temporary / "dist"
        work_path = temporary / "build-work"
        spec_path = temporary / "spec"
        binary_name = (
            "engelsiz-translate.exe" if os.name == "nt" else "engelsiz-translate"
        )
        run(
            venv_python,
            "-m",
            "PyInstaller",
            "--onedir",
            "--name",
            "engelsiz-translate",
            "--noconfirm",
            "--clean",
            "--distpath",
            str(dist_path),
            "--workpath",
            str(work_path),
            "--specpath",
            str(spec_path),
            "--exclude-module",
            "stanza",
            "--exclude-module",
            "spacy",
            "--exclude-module",
            "minisbd",
            "--exclude-module",
            "torch",
            "--exclude-module",
            "sacremoses",
            # readline pulls libreadline.so.8 (GPL) into the bundle for no
            # inference benefit; the worker never uses interactive input.
            "--exclude-module",
            "readline",
            # Neither the worker nor the argostranslate/CTranslate2 import
            # graph touches setuptools/pkg_resources at runtime (verified:
            # zero such modules after import); excluding keeps test-data
            # files like "Lorem ipsum.txt" and extra license surface out.
            "--exclude-module",
            "setuptools",
            "--exclude-module",
            "pkg_resources",
            "--collect-submodules",
            "argostranslate",
            str(WORKER),
            cwd=str(ROOT),
        )
        frozen = dist_path / "engelsiz-translate"
        binary = frozen / binary_name
        if not binary.is_file():
            raise SystemExit("Release blocked: PyInstaller produced no worker binary")
        desymlink_tree(frozen)

        notices = frozen / "THIRD-PARTY-NOTICES"
        if notices.exists():
            shutil.rmtree(notices)
        notices.mkdir(parents=True)
        dist_infos = {}
        env_site = site_packages(venv)
        for package in RUNTIME_PACKAGES:
            dist_infos[package] = license_notice(env_site, package, notices)
        # PyInstaller bootloader license: the wheel ships dist-info/licenses/
        # COPYING.txt (GPL-2.0-or-later WITH Bootloader-exception); collect
        # the whole build-tool dist-info for provenance, fail closed if the
        # license text is absent. No URL substitutes.
        build_tools = notices / "BUILD-TOOLS"
        for tool in ("pyinstaller", "pyinstaller-hooks-contrib", "altgraph"):
            infos = sorted(env_site.glob(tool.replace("-", "_") + "-*.dist-info")) + sorted(
                env_site.glob(tool.replace("_", "-") + "-*.dist-info"))
            if not infos:
                raise SystemExit(f"Release blocked: build tool dist-info missing: {tool}")
            target = build_tools / infos[0].name
            if target.exists():
                shutil.rmtree(target)
            shutil.copytree(infos[0], target, symlinks=False)
        pyi_infos = sorted((build_tools).glob("pyinstaller-*.dist-info"))
        if not pyi_infos or not (pyi_infos[0] / "licenses" / "COPYING.txt").is_file():
            raise SystemExit("Release blocked: PyInstaller COPYING.txt missing from wheel")

        collect_native_notices(frozen, binary_name, env_site, notices, venv_python,
                               python_license=args.python_license,
                               native_overrides=args.native_license)

        manifest = {
            "format": 1,
            "worker": "engelsiz-translate",
            "workerProtocol": 1,
            "binary": binary_name,
            "buildPlatform": sys.platform,
            "buildPython": subprocess.run(
                [venv_python, "-c", "import sys; print(sys.version.split()[0])"],
                check=True,
                capture_output=True,
                text=True,
            ).stdout.strip(),
            "packages": installed,
            "wheelPins": requirement_pins(RUNTIME_REQUIREMENTS),
            "buildPins": requirement_pins(BUILD_REQUIREMENTS),
            "buildTool": {
                "pyinstaller": installed.get("pyinstaller"),
                "pyinstaller-hooks-contrib": installed.get(
                    "pyinstaller-hooks-contrib"
                ),
                "altgraph": installed.get("altgraph"),
            },
            "distInfo": dist_infos,
            "glibc": glibc_version(),
            "linuxSharedLibraryAllowlist": shared_library_allowlist(frozen)
            if sys.platform == "linux"
            else [],
            "binarySha256": sha256_of(binary),
            "files": file_inventory(frozen),
        }
        (frozen / "translation-runtime-manifest.json").write_text(
            json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )

        if not args.skip_smoke:
            run(
                sys.executable,
                str(HERE / "smoke.py"),
                "--binary",
                str(binary),
                cwd=str(ROOT),
            )

        destination = output / ("translation-runtime-windows-amd64" if os.name == "nt" else "translation-runtime-linux-amd64")
        if destination.exists():
            shutil.rmtree(destination)
        shutil.copytree(frozen, destination, symlinks=False)

    size = sum(p.stat().st_size for p in destination.rglob("*") if p.is_file())
    count = sum(1 for p in destination.rglob("*") if p.is_file())
    print(f"runtime: {destination}")
    print(f"binary: {binary_name} sha256={sha256_of(destination / binary_name)}")
    print(f"files: {count} bytes: {size}")
    print(f"packages: {json.dumps(installed, sort_keys=True)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
