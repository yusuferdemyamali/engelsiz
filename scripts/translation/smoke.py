#!/usr/bin/env python3
"""Smoke-test a translation runtime worker binary without any models.

Starts the binary with an empty models root (temporary directory), sends one
malformed request plus one well-formed request for a missing model, and
expects exactly one protocol JSON error response per line. This proves the
frozen binary boots, speaks the JSONL protocol, and fails closed without
requiring the (separately downloaded) Argos models.

Usage: smoke.py --binary <engelsiz-translate[exe]> [--timeout 60]
Exit 0 on success, 1 with a diagnostic on stderr otherwise.
"""

import argparse
import json
import queue
import subprocess
import sys
import tempfile
import threading
from pathlib import Path


def check(binary, timeout):
    binary = Path(binary)
    if not binary.is_file():
        return f"Worker binary is missing: {binary}"
    with tempfile.TemporaryDirectory(prefix="engelsiz-translate-smoke-") as models:
        responses = queue.Queue()

        def read_responses():
            for line in process.stdout:
                responses.put(line)
            responses.put(None)

        try:
            process = subprocess.Popen(
                [str(binary), models],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                text=True,
                encoding="utf-8",
            )
        except OSError as error:
            return f"Cannot start worker binary: {error}"
        threading.Thread(target=read_responses, daemon=True).start()
        try:
            requests = [
                "not json at all",
                json.dumps(
                    {
                        "cmd": "translate",
                        "source_language": "tr",
                        "target_language": "en",
                        "text": "Merhaba",
                    }
                ),
            ]
            lines = []
            for request in requests:
                # Keep stdin open until each reply: communicate() would hide
                # readers incorrectly waiting for a full buffer or EOF.
                process.stdin.write(request + "\n")
                process.stdin.flush()
                response = responses.get(timeout=timeout)
                if response is None:
                    process.wait(timeout=15)
                    return "Worker exited before answering the request"
                lines.append(response)
            process.stdin.close()
            process.wait(timeout=15)
            if responses.get(timeout=5) is not None:
                return "Worker emitted an extra protocol response"
        except (subprocess.TimeoutExpired, queue.Empty, OSError):
            process.kill()
            process.wait(timeout=15)
            return "Worker did not answer within the timeout"
        if len(lines) != 2:
            return f"Expected 2 protocol responses, got {len(lines)}"
        try:
            first = json.loads(lines[0])
            second = json.loads(lines[1])
        except ValueError:
            return "Worker stdout is not JSONL protocol"
        if first.get("error", {}).get("code") != "invalid_request":
            return f"Malformed input must yield invalid_request, got {lines[0][:120]}"
        if second.get("error", {}).get("code") != "model_missing":
            return f"Missing model must yield model_missing, got {lines[1][:120]}"
        if process.returncode != 0:
            return f"Worker exit code after EOF is {process.returncode}, want 0"
    return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--timeout", type=float, default=120.0)
    args = parser.parse_args()
    failure = check(args.binary, args.timeout)
    if failure is not None:
        print(f"translation runtime smoke test failed: {failure}", file=sys.stderr)
        return 1
    print("translation runtime smoke test passed (no models required)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
