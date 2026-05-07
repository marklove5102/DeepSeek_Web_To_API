#!/usr/bin/env python3
"""Deploy ds2api binary to the prod host specified by env DST_HOST.

Compiles the linux/amd64 binary first (with ldflags injecting the version
from the repo-root VERSION file) so the deployed process reports the right
version on /admin/version. Set SKIP_BUILD=1 to use an existing binary at
build/deepseek-web-to-api-linux-amd64.

Lives in scripts/ alongside build-release-archives.sh. Build artifacts are
written to build/ (gitignored) regardless of where this script lives.
"""
from __future__ import annotations
import hashlib, io, os, subprocess, sys, time, paramiko

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")

HOST = os.environ.get("DST_HOST", "").strip()
USER = os.environ.get("DST_USER", "root").strip()
if not HOST:
    print("DST_HOST env required (e.g. DST_HOST=10.0.0.1 DST_PASSWORD=... python3 scripts/deploy_107.py)", file=__import__('sys').stderr)
    raise SystemExit(2)
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, ".."))
BUILD_DIR = os.path.join(REPO_ROOT, "build")
LOCAL_BIN = os.path.join(BUILD_DIR, "deepseek-web-to-api-linux-amd64")
VERSION_FILE = os.path.join(REPO_ROOT, "VERSION")
REMOTE_BIN = "/opt/deepseek-web-to-api/deepseek-web-to-api"
REMOTE_NEW = REMOTE_BIN + ".new"
SERVICE = "deepseek-web-to-api"
LDFLAGS_VAR = "DeepSeek_Web_To_API/internal/version.BuildVersion"

def sha256_file(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()

def read_version():
    """Returns the trimmed VERSION file contents, or '' if unreadable. The
    Go side strips a leading 'v' so we don't add one here — pass the bare
    semver."""
    try:
        with open(VERSION_FILE, "r", encoding="utf-8") as f:
            return f.read().strip()
    except OSError:
        return ""

def build_linux_binary(version):
    """Cross-compile linux/amd64 with -X injecting the version. Mirrors the
    flags used by scripts/build-release-archives.sh so manual deploys
    behave identically to CI release-artifacts builds — the deployed
    binary's /admin/version reports `version` (not 'dev')."""
    os.makedirs(BUILD_DIR, exist_ok=True)
    ldflags = f"-s -w -X {LDFLAGS_VAR}={version}" if version else "-s -w"
    cmd = [
        "go", "build",
        "-buildvcs=false", "-trimpath",
        "-ldflags", ldflags,
        "-o", LOCAL_BIN,
        "./cmd/DeepSeek_Web_To_API",
    ]
    env = os.environ.copy()
    env.update({"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"})
    print(f"[build] version={version or '<none>'} ldflags={ldflags!r}")
    subprocess.run(cmd, cwd=REPO_ROOT, env=env, check=True)
    print(f"[build] wrote {LOCAL_BIN} size={os.path.getsize(LOCAL_BIN)}")

def run(c, cmd, ok_codes=(0,)):
    _, o, e = c.exec_command(cmd, timeout=120)
    out = o.read().decode("utf-8", "replace")
    err = e.read().decode("utf-8", "replace")
    rc = o.channel.recv_exit_status()
    label = cmd if len(cmd) <= 80 else cmd[:77] + "..."
    print(f"[remote] $ {label}")
    if out.strip(): print(out.rstrip())
    if err.strip(): print(f"[stderr] {err.rstrip()}")
    if rc not in ok_codes:
        raise SystemExit(f"command failed (rc={rc})")
    return out

# Auth resolution: prefer ssh key (build/ds2api_deploy, gitignored) over
# password. Once setup_prod_keyauth.py has run, prod no longer accepts
# password auth and DST_PASSWORD is irrelevant.
SSH_KEY_FILE = os.environ.get("DST_SSH_KEY", "").strip()
if not SSH_KEY_FILE:
    default_key = os.path.join(BUILD_DIR, "ds2api_deploy")
    if os.path.exists(default_key):
        SSH_KEY_FILE = default_key

password = os.environ.get("DST_PASSWORD", "").strip() or None
if not SSH_KEY_FILE and not password:
    print("Either DST_SSH_KEY (or build/ds2api_deploy) must exist, or DST_PASSWORD env must be set", file=sys.stderr)
    sys.exit(2)

if os.environ.get("SKIP_BUILD") not in ("1", "true", "yes"):
    version = read_version()
    if not version:
        print(f"[warn] could not read {VERSION_FILE}; binary will report 'dev'", file=sys.stderr)
    build_linux_binary(version)
else:
    print("[build] SKIP_BUILD set — reusing existing binary")
    if not os.path.exists(LOCAL_BIN):
        print(f"SKIP_BUILD set but {LOCAL_BIN} does not exist", file=sys.stderr); sys.exit(2)

local_sha = sha256_file(LOCAL_BIN)
print(f"[local] sha256={local_sha} size={os.path.getsize(LOCAL_BIN)}")

c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
if SSH_KEY_FILE:
    print(f"[auth] using ssh key: {SSH_KEY_FILE}")
    c.connect(HOST, username=USER, key_filename=SSH_KEY_FILE, allow_agent=False, look_for_keys=False, timeout=30)
else:
    print("[auth] using password (consider running scripts/setup_prod_keyauth.py to switch to key-only)")
    c.connect(HOST, username=USER, password=password, allow_agent=False, look_for_keys=False, timeout=30)
try:
    run(c, "hostname; systemctl is-active " + SERVICE)
    sftp = c.open_sftp()
    sftp.put(LOCAL_BIN, REMOTE_NEW)
    sftp.chmod(REMOTE_NEW, 0o755)
    sftp.close()
    out = run(c, f"sha256sum {REMOTE_NEW}")
    remote_sha = out.split()[0] if out.split() else ""
    if remote_sha != local_sha:
        run(c, f"rm -f {REMOTE_NEW}")
        raise SystemExit(f"sha256 mismatch local={local_sha} remote={remote_sha}")
    print("[verify] sha256 ok")
    ts = int(time.time())
    backup = f"{REMOTE_BIN}.bak.{ts}"
    run(c,
        f"systemctl stop {SERVICE} && "
        f"(test -f {REMOTE_BIN} && cp -p {REMOTE_BIN} {backup} || true) && "
        f"mv {REMOTE_NEW} {REMOTE_BIN} && "
        f"chmod +x {REMOTE_BIN} && "
        f"systemctl start {SERVICE}")
    time.sleep(3)
    run(c, f"systemctl is-active {SERVICE}")
    run(c, f"curl -sS -o /dev/null -w 'HTTP %{{http_code}} (%{{time_total}}s)\\n' --max-time 5 http://127.0.0.1:5001/healthz")
    run(c, f"ps -o pid,etimes,cmd -p $(pgrep -f deepseek-web-to-api | head -1)")
    print(f"[done] backup at {backup}")
finally:
    c.close()
