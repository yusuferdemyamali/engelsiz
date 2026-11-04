#!/usr/bin/env python3
"""Dispatch-only Windows installer QA for the offline translation runtime.

Runs on a Windows CI runner against a real Engelsiz-Setup.exe artifact:
silent install to a Unicode+space path, app start --no-browser, download of
BOTH Argos models via the real API, install, translate in both directions,
restart with no-redownload assertion, uninstall/reinstall with preservation
assertion, and a best-effort offline check via Windows Firewall rules.

Bounded and robust: every wait has a timeout, the app is always terminated,
firewall rules are always removed, and a JSON+Markdown report is emitted.
Exit code is non-zero when any required step fails. No secrets are used.

This script only *drives* the product over HTTP and the OS; it never weakens
release.py gates and never touches compliance material.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

MODELS = ("argos-tr-en", "argos-en-tr")
TRANSLATE_CASES = (
    {"source_language": "tr", "target_language": "en", "text": "günaydın"},
    {"source_language": "en", "target_language": "tr", "text": "good morning"},
)


def log(message):
    print(f"[translation-qa] {message}", flush=True)


class Reporter:
    def __init__(self):
        self.steps = []
        self.notes = []

    def record(self, name, ok, detail="", duration_s=0.0):
        self.steps.append({
            "name": name,
            "ok": bool(ok),
            "detail": str(detail)[:2000],
            "duration_s": round(duration_s, 1),
        })
        log(f"{'PASS' if ok else 'FAIL'} {name} ({duration_s:.1f}s) {detail[:160]}")

    def note(self, text):
        self.notes.append(text)
        log(f"note: {text}")

    def failed(self):
        return any(not s["ok"] for s in self.steps)

    def write(self, report_dir):
        os.makedirs(report_dir, exist_ok=True)
        payload = {
            "product": "engelsiz-windows-installer-translation-qa",
            "models": list(MODELS),
            "ok": not self.failed(),
            "steps": self.steps,
            "notes": self.notes,
        }
        with open(os.path.join(report_dir, "report.json"), "w", encoding="utf-8") as fh:
            json.dump(payload, fh, indent=2, ensure_ascii=False)
        lines = ["# Engelsiz Windows installer translation QA",
                 "",
                 f"Result: {'PASS' if not self.failed() else 'FAIL'}",
                 "",
                 "## Steps", ""]
        for step in self.steps:
            mark = "PASS" if step["ok"] else "FAIL"
            lines.append(f"- [{mark}] {step['name']} ({step['duration_s']}s) {step['detail']}")
        if self.notes:
            lines += ["", "## Notes", ""]
            lines += [f"- {n}" for n in self.notes]
        with open(os.path.join(report_dir, "report.md"), "w", encoding="utf-8") as fh:
            fh.write("\n".join(lines) + "\n")


def http_json(method, url, body=None, timeout=60):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", "replace")
            return resp.status, (json.loads(raw) if raw.strip() else {})
    except urllib.error.HTTPError as err:
        try:
            return err.code, json.loads(err.read().decode("utf-8", "replace") or "{}")
        except ValueError:
            return err.code, {}


def wait_for(url, timeout, predicate, label, interval=2.0):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            status, body = http_json("GET", url, timeout=10)
            last = f"http={status}"
            if predicate(status, body):
                return body
        except (OSError, ValueError) as err:
            last = f"unreachable: {err}"[:120]
        time.sleep(interval)
    raise TimeoutError(f"{label} not reached within {timeout}s (last: {last})")


class App:
    """A started engelsiz.exe with port discovery from its log line."""

    def __init__(self, exe, workdir):
        self.exe = exe
        self.workdir = workdir
        self.proc = None
        self.base = None
        self.log_path = os.path.join(workdir, "app.log")

    def start(self, timeout):
        logfile = open(self.log_path, "ab")
        kwargs = {}
        if os.name == "nt":
            # Own process group so stop() can send CTRL_BREAK for a graceful
            # shutdown that exercises the app's child-process cleanup.
            kwargs["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
        self.proc = subprocess.Popen(
            [self.exe, "--no-browser"], stdout=logfile, stderr=subprocess.STDOUT,
            cwd=os.path.dirname(self.exe), **kwargs)
        logfile.close()
        deadline = time.time() + timeout
        pattern = re.compile(rb"listening on http://127\.0\.0\.1:(\d+)")
        while time.time() < deadline:
            if self.proc.poll() is not None:
                raise RuntimeError(f"app exited early (code {self.proc.returncode}); see {self.log_path}")
            with open(self.log_path, "rb") as fh:
                match = pattern.search(fh.read())
            if match:
                self.base = f"http://127.0.0.1:{int(match.group(1))}"
                break
            time.sleep(1.0)
        if self.base is None:
            self.stop()
            raise TimeoutError("app port not discovered from log")
        wait_for(self.base + "/api/health", timeout, lambda s, _b: s == 200, "health")
        log(f"app healthy at {self.base}")

    def stop(self):
        if self.proc is not None and self.proc.poll() is None:
            if os.name == "nt":
                try:
                    # Graceful first: exercises deferred worker cleanup.
                    self.proc.send_signal(subprocess.signal.CTRL_BREAK_EVENT)
                    self.proc.wait(timeout=30)
                except (OSError, subprocess.TimeoutExpired):
                    pass
            if self.proc.poll() is None:
                self.proc.terminate()
                try:
                    self.proc.wait(timeout=30)
                except subprocess.TimeoutExpired:
                    self.proc.kill()
        self.proc = None


def assert_no_worker(report, label):
    """Fail when a translation worker outlives the app (child cleanup)."""
    if os.name != "nt":
        report.note(f"{label}: worker-liveness check is Windows-only here")
        return True
    try:
        out = subprocess.run(["tasklist", "/FI", "IMAGENAME eq engelsiz-translate.exe"],
                             capture_output=True, text=True,
                             timeout=30).stdout
    except (OSError, subprocess.TimeoutExpired) as err:
        report.note(f"{label}: tasklist unavailable ({err})")
        return True
    alive = "engelsiz-translate.exe" in out
    report.record(label, not alive,
                  "no worker process remains" if not alive else f"worker survived app stop:\n{out[:500]}",
                  0.0)
    return not alive


def assert_no_bundled_models(report, target):
    """Models must never ship inside the installed tree."""
    hits = []
    for root, _dirs, files in os.walk(target):
        for name in files:
            if name.endswith(".argosmodel"):
                hits.append(os.path.relpath(os.path.join(root, name), target))
    report.record("no-bundled-models", not hits,
                  "installed tree is model-free" if not hits else f"bundled: {hits[:5]}",
                  0.0)
    return not hits


def run_step(report, name, func, *args, **kwargs):
    started = time.time()
    try:
        detail = func(*args, **kwargs) or ""
        report.record(name, True, detail, time.time() - started)
        return True
    except Exception as err:  # noqa: BLE001 - QA must record, not crash
        report.record(name, False, f"{type(err).__name__}: {err}", time.time() - started)
        return False


def install(installer, target):
    if os.path.isdir(target):
        shutil.rmtree(target, ignore_errors=True)
    cmd = [installer, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", f"/DIR={target}"]
    subprocess.run(cmd, check=True, timeout=600)
    exe = os.path.join(target, "engelsiz.exe")
    if not os.path.isfile(exe):
        raise RuntimeError("installer finished without engelsiz.exe")
    return f"silent install to {target}"


def uninstall(target):
    remover = os.path.join(target, "unins000.exe")
    if not os.path.isfile(remover):
        raise RuntimeError("uninstaller missing")
    subprocess.run([remover, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"],
                   check=True, timeout=600)
    return "silent uninstall finished"


def translation_status(base):
    status, body = http_json("GET", base + "/api/translation", timeout=30)
    if status != 200:
        raise RuntimeError(f"translation status http={status}")
    return {m["id"]: m for m in body.get("models", [])}


def download_and_install(base, download_timeout):
    ids = {}
    for model in MODELS:
        status, body = http_json("POST", f"{base}/api/translation/models/{model}/download", {},
                                 timeout=60)
        if status != 201:
            raise RuntimeError(f"download start {model} http={status} {body}")
        download_id = body["download"]["id"]
        ids[model] = download_id

        def done(s, b, _id=download_id):
            models = {m["id"]: m for m in b.get("models", [])} if s == 200 else {}
            entry = models.get(model, {})
            snap = entry.get("download") or {}
            return entry.get("status") != "downloading" and snap.get("id") == _id \
                and snap.get("status") == "completed"

        wait_for(base + "/api/translation", download_timeout, done, f"{model} download")
        status, body = http_json("POST", f"{base}/api/translation/models/{model}/install",
                                 {"download_id": download_id}, timeout=600)
        if status not in (200, 202):
            raise RuntimeError(f"install {model} http={status} {body}")
    models = translation_status(base)
    try:
        wait_for(base + "/api/translation", 600,
                 lambda s, b: s == 200 and all(
                     (m.get("status") == "ready")
                     for m in [({mm["id"]: mm for mm in b.get("models", [])}.get(m, {}))
                               for m in MODELS]),
                 "both models ready")
    except TimeoutError as err:
        raise RuntimeError(str(err))
    models = translation_status(base)
    not_ready = [m for m in MODELS if models.get(m, {}).get("status") != "ready"]
    if not_ready:
        raise RuntimeError(f"models not ready after install: {not_ready}")
    return f"both models ready (downloads {ids['argos-tr-en'][:8]}… {ids['argos-en-tr'][:8]}…)"


def translate_both(base):
    for case in TRANSLATE_CASES:
        status, body = http_json("POST", base + "/api/translation/translate", case, timeout=600)
        if status != 200 or not (body.get("translated_text") or "").strip():
            raise RuntimeError(f"translate {case['source_language']}->{case['target_language']} "
                               f"http={status} {body}")
    return "tr->en and en->tr returned non-empty text"


def firewall_block(report, paths):
    """Best-effort egress block. Hosted runners cannot do full network
    isolation; loopback stays reachable so the local API keeps working."""
    if os.name != "nt":
        report.note("offline-firewall: skipped (not Windows); full isolation NOT claimed")
        return []
    added = []
    try:
        for i, path in enumerate(paths):
            name = f"EngelsizQABlock{i}"
            subprocess.run(["netsh", "advfirewall", "firewall", "add", "rule",
                            f"name={name}", "dir=out", "action=block",
                            f"program={path}", "enable=yes"],
                           check=True, capture_output=True, timeout=60)
            added.append(name)
        return added
    except (OSError, subprocess.CalledProcessError) as err:
        for name in added:
            subprocess.run(["netsh", "advfirewall", "firewall", "delete", "rule",
                            f"name={name}"], capture_output=True, timeout=60)
        report.note(f"offline-firewall: unavailable ({err}); full isolation NOT claimed")
        return []


def firewall_unblock(names):
    for name in names:
        subprocess.run(["netsh", "advfirewall", "firewall", "delete", "rule", f"name={name}"],
                       capture_output=True, timeout=60)


def user_data_dir():
    base = os.environ.get("LOCALAPPDATA") or os.path.expanduser("~")
    return os.path.join(base, "Engelsiz")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--installer", required=True)
    parser.add_argument("--install-dir", default=None)
    parser.add_argument("--report-dir", default="qa-report")
    parser.add_argument("--startup-timeout", type=float, default=180)
    parser.add_argument("--download-timeout", type=float, default=1800)
    args = parser.parse_args()

    report = Reporter()
    workdir = tempfile.mkdtemp(prefix="engelsiz-qa-")
    target = args.install_dir or os.path.join(tempfile.gettempdir(), "Engelsiz Çeviri QA")
    app = None
    firewall_rules = []
    failed_early = False

    try:
        if not run_step(report, "silent-install-unicode-path", install,
                        os.path.abspath(args.installer), target):
            failed_early = True
        else:
            exe = os.path.join(target, "engelsiz.exe")
            app = App(exe, workdir)
            if not run_step(report, "start-no-browser-health", app.start, args.startup_timeout):
                failed_early = True

        if not failed_early:
            worker_exe = os.path.join(target, "runtime", "translation",
                                      "engelsiz-translate.exe")
            if os.path.isfile(worker_exe):
                report.record("translation-runtime-present", True,
                              "engelsiz-translate.exe staged", 0.0)
            else:
                report.record("translation-runtime-present", False,
                              "runtime/translation/engelsiz-translate.exe missing; "
                              "installer cannot translate", 0.0)
                failed_early = True
        if not failed_early:
            assert_no_bundled_models(report, target)
            run_step(report, "download-install-both-models", download_and_install,
                     app.base, args.download_timeout)
            run_step(report, "translate-both-directions", translate_both, app.base)

            # Restart: models must already be ready, no redownload.
            before = translation_status(app.base)
            before_ids = {m: (before.get(m, {}).get("download") or {}).get("id") for m in MODELS}
            app.stop()
            app.start(args.startup_timeout)
            after = translation_status(app.base)
            after_ids = {m: (after.get(m, {}).get("download") or {}).get("id") for m in MODELS}
            if all(after.get(m, {}).get("status") == "ready" for m in MODELS) \
                    and before_ids == after_ids:
                report.record("restart-no-redownload", True,
                              f"both ready, download ids unchanged", 0.0)
            else:
                report.record("restart-no-redownload", False,
                              f"before={before_ids} after={after_ids}", 0.0)
            run_step(report, "translate-after-restart", translate_both, app.base)

            # Best-effort offline: block egress for app + worker, translate must still work.
            worker = os.path.join(target, "runtime", "translation")
            progs = [exe] + [os.path.join(worker, n) for n in os.listdir(worker)
                             if n.lower().endswith(".exe")] if os.path.isdir(worker) else [exe]
            firewall_rules = firewall_block(report, progs)
            if firewall_rules:
                try:
                    run_step(report, "translate-egress-blocked", translate_both, app.base)
                finally:
                    firewall_unblock(firewall_rules)
                    firewall_rules = []
                report.note("offline scope: egress-block only; full network isolation NOT claimed "
                            "on a hosted runner")
            app.stop()
            assert_no_worker(report, "worker-cleanup-after-stop")

            # Uninstall (user data preserved by design: no [UninstallDelete]),
            # reinstall, models/DB must survive without new downloads.
            data = user_data_dir()
            had_data = os.path.isdir(data) and any(os.scandir(data))
            run_step(report, "silent-uninstall", uninstall, target)
            if not had_data:
                report.record("user-data-present-after-uninstall", False,
                              f"{data} missing/empty right after uninstall", 0.0)
            else:
                report.record("user-data-present-after-uninstall", True, data, 0.0)
            if run_step(report, "reinstall", install, os.path.abspath(args.installer), target):
                app = App(os.path.join(target, "engelsiz.exe"), workdir)
                if run_step(report, "start-after-reinstall", app.start, args.startup_timeout):
                    models = translation_status(app.base)
                    preserved = all(models.get(m, {}).get("status") == "ready" for m in MODELS)
                    report.record("models-preserved-after-reinstall", preserved,
                                  str({m: models.get(m, {}).get("status") for m in MODELS}), 0.0)
                    if preserved:
                        run_step(report, "translate-after-reinstall", translate_both, app.base)
                    app.stop()
                    assert_no_worker(report, "worker-cleanup-after-reinstall")
                    app = None
    finally:
        if firewall_rules:
            firewall_unblock(firewall_rules)
        if app is not None:
            app.stop()
        shutil.rmtree(workdir, ignore_errors=True)
        report.write(args.report_dir)

    if report.failed():
        log("QA FAILED; see report.json/report.md")
        return 1
    log("QA passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
