#!/usr/bin/env python3
"""Engelsiz offline translation worker (persistent JSONL protocol).

Protocol (owned by the Go parent process):
  argv[1] = absolute models root containing fixed subdirs
    ``argos-tr-en`` (Turkish -> English) and ``argos-en-tr``
    (English -> Turkish), each with metadata.json plus the Argos model files
    (model/, sentencepiece.model or bpe.model) as installed from .argosmodel.
  stdin  = one JSON object per line:
    {"cmd":"translate","source_language":"tr"|"en",
     "target_language":"en"|"tr","text":string}
  stdout = exactly one flushed JSON object per input line:
    {"translated_text":string} or {"error":{"code":string,"message":string}}

Notes:
  - stdout is exclusively protocol output. Nothing else may be written there.
  - No source text is logged, stored, or echoed anywhere except the
    translated response itself.
  - The worker never uses the network: translators are lazy-cached CPU
    CTranslate2 translators driven through the real argostranslate package
    (Package/tokenizer/apply_packaged_translation) with an explicit offline
    sentence splitter, and sockets are disabled defense-in-depth after imports.
  - stdin EOF exits 0 gracefully.
"""

import json
import os
import re
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# Vendored compatibility patch (source-visible; no binary modification).
#
# Upstream argostranslate 1.11.0 (MIT) unconditionally imports stanza and
# minisbd at argostranslate/sbd.py import time, which drags in the torch/CUDA
# stack and enables on-demand model downloads. Upstream's optional-import
# fix (master 17ed1a9b05) is NOT in any released version, and the NONE chunk
# type is unimplemented in 1.11.0, so this worker pre-registers lightweight
# stand-ins for those two optional sentence-boundary backends before
# importing argostranslate. They are never used: every translation below
# goes through the real argostranslate Package/tokenizer/CTranslate2 code
# path with OfflineSentencizer instead. Installed wheels stay pristine and
# hash-pinned (see scripts/translation/requirements-runtime.txt); nothing is
# patched inside site-packages or any frozen binary.
#
# minisbd is additionally a hard licensing exclusion (AGPL-3.0), so even the
# real library must not enter the frozen runtime.
# ---------------------------------------------------------------------------
import types as _types


def _install_sbd_standins():
    """Unconditional stand-ins for optional SBD backends (no imports that
    could fetch; the real stanza/minisbd packages are never installed)."""
    if "stanza" not in sys.modules:
        sys.modules["stanza"] = _types.ModuleType("stanza")
    if "minisbd" not in sys.modules:
        module = _types.ModuleType("minisbd")

        class _SBDetect:  # pragma: no cover - never instantiated
            def __init__(self, *args, **kwargs):
                raise RuntimeError("minisbd is disabled in the offline worker")

        module.SBDetect = _SBDetect
        module.models = _types.SimpleNamespace(cache_dir="", list_models=lambda: [])
        sys.modules["minisbd"] = module
    if "minisbd.models" not in sys.modules:
        sys.modules["minisbd.models"] = sys.modules["minisbd"].models


def _deny_sockets():
    """Deny all Python-level socket use (defense-in-depth, before any import
    that could fetch). Translators are local CTranslate2 + sentencepiece
    calls; nothing here legitimately opens a socket."""

    def _blocked(*args, **kwargs):
        raise RuntimeError("network access is disabled in the offline worker")

    import socket as _socket
    import urllib.request as _urllib

    _socket.socket = _blocked  # type: ignore[assignment]
    _socket.create_connection = _blocked  # type: ignore[assignment]
    _urllib.urlopen = _blocked  # type: ignore[assignment]


_install_sbd_standins()
_deny_sockets()

# Keep argostranslate's own data/config/cache directories out of the user's
# home: nothing here needs persistence.
import tempfile as _tempfile

for _var, _sub in (
    ("XDG_DATA_HOME", "data"),
    ("XDG_CONFIG_HOME", "config"),
    ("XDG_CACHE_HOME", "cache"),
):
    if not os.environ.get(_var):
        _dir = Path(_tempfile.gettempdir()) / f"engelsiz-argos-{_sub}"
        try:
            _dir.mkdir(parents=True, exist_ok=True)
        except OSError:
            continue
        os.environ[_var] = str(_dir)

os.environ.setdefault("ARGOS_DEVICE_TYPE", "cpu")

try:
    from argostranslate import package as _argos_package
    from argostranslate import translate as _argos_translate
except Exception as error:  # startup failure is fatal; Go reports the exit
    sys.stderr.write(f"engelsiz-translate: failed to load argostranslate: {error}\n")
    sys.stderr.flush()
    raise SystemExit(1)


def _disable_network():
    """Deny argostranslate-level networking after import (second phase).

    Socket denial already happened before imports; this blocks the
    library's own download/update helpers too. Nothing is ever re-enabled
    and no model is (re-)downloaded on any request path.
    """
    import urllib.request as _urllib

    def _blocked(*args, **kwargs):
        raise RuntimeError("network access is disabled in the offline worker")

    _urllib.urlopen = _blocked  # type: ignore[assignment]
    _argos_package.urllib.request.urlopen = _blocked  # type: ignore[assignment]
    _argos_package.networking.get = _blocked  # type: ignore[assignment]
    _argos_package.networking.get_from = _blocked  # type: ignore[assignment]
    _argos_package.update_package_index = _blocked  # type: ignore[assignment]


_disable_network()

# ---------------------------------------------------------------------------
# Limits and error codes.
# ---------------------------------------------------------------------------
MAX_TEXT_CHARS = 5000
MAX_SENTENCE_CHARS = 400
MAX_LINE_CHARS = 65536
MAX_DISCARD_CHARS = 1 << 20

ERROR_INVALID_REQUEST = "invalid_request"
ERROR_UNSUPPORTED_PAIR = "unsupported_pair"
ERROR_MODEL_MISSING = "model_missing"
ERROR_MODEL_INVALID = "model_invalid"
ERROR_TEXT_TOO_LONG = "text_too_long"
ERROR_TRANSLATE_FAILED = "translate_failed"

DIRECTION_DIRS = {
    ("tr", "en"): "argos-tr-en",
    ("en", "tr"): "argos-en-tr",
}

_SENTENCE_END = re.compile(r"(?<=[.!?\u2026])\s+")

_GENERIC_MESSAGES = {
    ERROR_INVALID_REQUEST: "Invalid translation request.",
    ERROR_UNSUPPORTED_PAIR: "Unsupported language pair.",
    ERROR_MODEL_MISSING: "Translation model is not installed.",
    ERROR_MODEL_INVALID: "Translation model is invalid.",
    ERROR_TEXT_TOO_LONG: "Translation text exceeds the size limit.",
    ERROR_TRANSLATE_FAILED: "Translation failed.",
}


class OfflineSentencizer:
    """Explicit offline sentence splitter (no models, no downloads).

    Only implements the duck-typed interface consumed by
    argostranslate.translate.apply_packaged_translation
    (``split_sentences(text)``). Paragraph/newline structure is handled by
    the caller, which translates paragraph by paragraph.
    """

    def __init__(self, pkg=None):
        self.pkg = pkg

    def split_sentences(self, text):
        if not text:
            return [text]
        sentences = []
        for chunk in _SENTENCE_END.split(text):
            chunk = chunk.strip()
            if not chunk:
                continue
            while len(chunk) > MAX_SENTENCE_CHARS:
                cut = chunk.rfind(" ", 0, MAX_SENTENCE_CHARS)
                if cut <= 0:
                    cut = MAX_SENTENCE_CHARS
                sentences.append(chunk[:cut])
                chunk = chunk[cut:].strip()
            if chunk:
                sentences.append(chunk)
        return sentences or [text]

    def __str__(self):
        return "OfflineSentencizer"


def _error(code):
    return {"error": {"code": code, "message": _GENERIC_MESSAGES[code]}}


def _load_direction(models_root, source, target):
    """Load (and cache at call site) one translation direction.

    Returns (translator_holder, error_code). The holder has ``pkg``,
    ``translator`` (ctranslate2.Translator) and ``sentencizer``.
    """
    subdir = DIRECTION_DIRS[(source, target)]
    package_dir = models_root / subdir
    if not package_dir.is_dir():
        return None, ERROR_MODEL_MISSING
    metadata_path = package_dir / "metadata.json"
    if not metadata_path.is_file():
        return None, ERROR_MODEL_MISSING
    try:
        metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None, ERROR_MODEL_INVALID
    if metadata.get("from_code") != source or metadata.get("to_code") != target:
        return None, ERROR_MODEL_INVALID
    model_dir = package_dir / "model"
    if not model_dir.is_dir():
        return None, ERROR_MODEL_MISSING
    if not (package_dir / "sentencepiece.model").is_file() and not (
        package_dir / "bpe.model"
    ).is_file():
        return None, ERROR_MODEL_MISSING
    try:
        pkg = _argos_package.Package(package_dir)
    except Exception:
        return None, ERROR_MODEL_INVALID
    if getattr(pkg, "tokenizer", None) is None:
        return None, ERROR_MODEL_INVALID
    if type(pkg.tokenizer).__name__ == "BPETokenizer":
        # Legacy BPE (.bpe.model) packages need the sacremoses stack, which
        # is deliberately not frozen. Fail closed at load, not mid-request.
        return None, ERROR_MODEL_INVALID
    try:
        import ctranslate2

        translator = ctranslate2.Translator(
            str(model_dir),
            device="cpu",
            inter_threads=1,
            intra_threads=0,
            compute_type="default",
        )
    except Exception:
        return None, ERROR_MODEL_INVALID
    holder = _types.SimpleNamespace(
        pkg=pkg, translator=translator, sentencizer=OfflineSentencizer(pkg)
    )
    return holder, None


def _translate_paragraph(holder, paragraph):
    """Translate one paragraph, preserving its content when trivial."""
    if paragraph.strip() == "":
        return paragraph
    hypotheses = _argos_translate.apply_packaged_translation(
        holder.pkg, paragraph, holder.translator, holder.sentencizer, 1
    )
    return hypotheses[0].value


def _handle_request(models_root, cache, request):
    if not isinstance(request, dict):
        return _error(ERROR_INVALID_REQUEST)
    if request.get("cmd") != "translate":
        return _error(ERROR_INVALID_REQUEST)
    source = request.get("source_language")
    target = request.get("target_language")
    text = request.get("text")
    if (
        source not in ("tr", "en")
        or target not in ("tr", "en")
        or not isinstance(text, str)
    ):
        return _error(ERROR_INVALID_REQUEST)
    if (source, target) not in DIRECTION_DIRS:
        return _error(ERROR_UNSUPPORTED_PAIR)
    if len(text) > MAX_TEXT_CHARS:
        return _error(ERROR_TEXT_TOO_LONG)
    if text == "":
        return {"translated_text": ""}
    key = (source, target)
    holder = cache.get(key)
    if holder is None:
        holder, error_code = _load_direction(models_root, source, target)
        if error_code is not None:
            return _error(error_code)
        cache[key] = holder
    try:
        paragraphs = [
            _translate_paragraph(holder, paragraph)
            for paragraph in text.split("\n")
        ]
    except Exception:
        return _error(ERROR_TRANSLATE_FAILED)
    return {"translated_text": "\n".join(paragraphs)}


def _make_reader(stream):
    """Read bounded JSONL without waiting for a full buffer or stdin EOF."""

    def read_line():
        line = stream.readline(MAX_LINE_CHARS + 1)
        if len(line) <= MAX_LINE_CHARS:
            return line
        discarded = 0
        while line and not line.endswith("\n"):
            line = stream.readline(8192)
            discarded += len(line)
            if discarded > MAX_DISCARD_CHARS:
                raise ValueError("protocol line exceeds discard limit")
        return None

    return read_line


def main(argv):
    if len(argv) != 2 or not argv[1]:
        sys.stderr.write("usage: engelsiz-translate <absolute-models-root>\n")
        return 2
    models_root = Path(argv[1])
    if not models_root.is_absolute():
        sys.stderr.write("engelsiz-translate: models root must be absolute\n")
        return 2
    # The Go parent always speaks UTF-8 JSONL (Turkish text). Force UTF-8 on
    # stdio so Windows console code pages can neither corrupt nor crash on
    # non-ASCII translations.
    try:
        sys.stdin.reconfigure(encoding="utf-8", errors="strict")
        sys.stdout.reconfigure(encoding="utf-8", errors="strict")
    except Exception:
        pass
    cache = {}
    stdin = sys.stdin
    stdout = sys.stdout
    read_line = _make_reader(stdin)
    while True:
        try:
            line = read_line()
        except (ValueError, OSError):
            return 0
        if line == "":
            return 0
        if line is None:
            response = _error(ERROR_INVALID_REQUEST)
        else:
            line = line.strip()
            if not line:
                continue
            try:
                request = json.loads(line)
            except ValueError:
                response = _error(ERROR_INVALID_REQUEST)
            else:
                response = _handle_request(models_root, cache, request)
        try:
            stdout.write(json.dumps(response, ensure_ascii=False) + "\n")
            stdout.flush()
        except (BrokenPipeError, OSError, ValueError):
            return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
