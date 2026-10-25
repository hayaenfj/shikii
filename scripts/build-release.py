#!/usr/bin/env python3
"""Builds the shikii release files. Run at the repository root with no uncommitted changes in the working tree.

  python3 -I scripts/build-release.py <output folder>

The version is Version in version.go, and the build source is the HEAD commit. Puts tar.gz files for 4 platforms and
SHA256SUMS in <output folder>/assets/, and keeps the result of go version -m in <output folder>/<name>-<binary>-build.txt.
"""
import gzip, hashlib, json, os, re, shutil, subprocess, sys, tarfile, tempfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

if len(sys.argv) != 2:
    sys.exit("usage: python3 -I scripts/build-release.py <output folder>")
source = Path.cwd()
out = Path(sys.argv[1]).resolve()
commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
if subprocess.check_output(["git", "status", "--porcelain"], text=True):
    sys.exit("the working tree has uncommitted changes. Commit first, then build")
if out.exists() and any(out.iterdir()):
    sys.exit(f"the output folder {out} is not empty. Pass an empty folder so that leftovers of an earlier version are not shipped")
# Copy HEAD into a temp folder and build there, so that ignored files (.DS_Store etc.) do not get embedded
work = Path(tempfile.mkdtemp(prefix="shikii-release-"))
subprocess.run(["git", "clone", "-q", "--no-local", str(source), str(work / "src")], check=True)
subprocess.run(["git", "-C", str(work / "src"), "checkout", "-q", "--detach", commit], check=True)
repo = work / "src"
os.chdir(repo)
version = re.search(r'const Version = "([^"]+)"', (repo / "version.go").read_text(encoding="utf-8")).group(1)
problems = []
# The Go version to build with is go.mod's toolchain (the version with vulnerabilities fixed). Stop if built with another version, e.g. via GOTOOLCHAIN=local (audit 0.8.2-2 B1)
toolchain = re.search(r"^toolchain (go\S+)$", (repo / "go.mod").read_text(encoding="utf-8"), re.M)
if not toolchain:
    problems.append("go.mod has no toolchain line")
if json.loads((repo / ".claude-plugin/plugin.json").read_text(encoding="utf-8")).get("version") != version:
    problems.append(".claude-plugin/plugin.json's version is not " + version)
for f in ["README.md", "docs/readme/README_ja.md"]:
    for v in re.findall(r"badge/version-([0-9.]+)-", (repo / f).read_text(encoding="utf-8")):
        if v != version:
            problems.append(f"{f}: the version badge says {v}")
for f in ["CHANGELOG.md", "CHANGELOG_ja.md"]:
    head = next(l for l in (repo / f).read_text(encoding="utf-8").splitlines() if l.startswith("## "))
    if not head.startswith(f"## {version} — ") or re.search(r"unreleased|未公開", head, re.I):
        problems.append(f"{f}: the first heading is not in released form: {head}")
tag = subprocess.run(["git", "-C", str(source), "rev-parse", "-q", "--verify", f"refs/tags/v{version}^{{commit}}"], capture_output=True, text=True).stdout.strip()
if tag and tag != commit:
    problems.append(f"tag v{version} points to {tag[:7]}, not HEAD {commit[:7]}")
if problems:
    shutil.rmtree(work)
    sys.exit("not ready for release:\n  " + "\n  ".join(problems))
vcs_time = int(subprocess.check_output(["git", "show", "-s", "--format=%ct", commit], text=True))
out.mkdir(parents=True, exist_ok=True)
assets = out / "assets"
assets.mkdir(exist_ok=True)
install = f"""Installing and updating shikii {version}

Note: updating from 0.6.0 or earlier deletes everything inside .shikii/ of each initialized project and initializes it again.
Earlier conversations, lessons, truths and calibrations are deleted. Save anything you want to keep first.

If shikii update already works:
  shikii update

To place the files by hand, put the bundled shikii and shikii-readout into the same folder on your PATH.
Check the archive hash with SHA256SUMS, and do not overwrite a binary in use directly: prepare it under another name, then replace it with mv (overwriting the same file makes every later start fail with Killed: 9 on macOS).
Keep the 4 license files in shikii-licenses/ next to the binaries.
Then run:
  shikii hook install --yes

Initialize once, only in the projects that use it:
  shikii --init

After updating, start a new agent session and restart the running page and MCP.
This updates this version's skill and hook.
The recommended model is TypeSafe's Jev (environment variable TYPESAFE_API_KEY). If you cannot set a key, llama.cpp. See "必要なもの" (Requirements) in README_ja.md.
shikii itself is licensed under Apache-2.0 OR MIT. For third-party terms, see THIRD-PARTY-NOTICES.
"""
BUNDLED = [("LICENSE", "LICENSE"), ("LICENSE-APACHE", "LICENSE-APACHE"), ("LICENSE-MIT", "LICENSE-MIT"),
           ("THIRD-PARTY-NOTICES", "THIRD-PARTY-NOTICES"), ("CHANGELOG.md", "CHANGELOG.md"),
           ("CHANGELOG_ja.md", "CHANGELOG_ja.md"), ("docs/readme/README_ja.md", "README_ja.md")]
host_os, host_arch = subprocess.check_output(["go", "env", "GOHOSTOS", "GOHOSTARCH"], text=True).split()
TARGETS = [("darwin", "arm64"), ("darwin", "amd64"), ("linux", "arm64"), ("linux", "amd64")]


def build(target):
    system, arch = target
    name = f"shikii_{version}_{system}_{arch}"
    stage = out / name
    stage.mkdir(exist_ok=True)
    env = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED="0")
    for binary in ["shikii", "shikii-readout"]:
        cmd = ["go", "build", "-trimpath", "-buildvcs=true", "-ldflags=-s -w", "-o", str(stage / binary), "./cmd/" + binary]
        r = subprocess.run(cmd, env=env, capture_output=True, text=True)
        if r.returncode:
            raise RuntimeError(r.stderr)
        info = subprocess.check_output(["go", "version", "-m", str(stage / binary)], text=True)
        if not ("vcs.revision=" + commit in info and "vcs.modified=false" in info and "GOOS=" + system in info and "GOARCH=" + arch in info):
            raise RuntimeError(f"{name}/{binary}: the build source information does not match\n{info}")
        if not info.splitlines()[0].endswith(": " + toolchain.group(1)):
            raise RuntimeError(f"{name}/{binary}: not built with {toolchain.group(1)} (go.mod's toolchain)\n{info.splitlines()[0]}")
        (out / (name + "-" + binary + "-build.txt")).write_text(info)
    for source, dest in BUNDLED:
        shutil.copy2(repo / source, stage / dest)
    (stage / "INSTALL.txt").write_text(install)
    archive = assets / (name + ".tar.gz")
    def normalize(info):
        info.uid = info.gid = 0
        info.uname = info.gname = ""
        info.mtime = vcs_time
        info.mode = 0o755 if info.name in ("shikii", "shikii-readout") else 0o644
        return info
    with open(archive, "wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as gz, tarfile.open(fileobj=gz, mode="w", format=tarfile.PAX_FORMAT) as tar:
        for file in sorted(stage.iterdir()):
            if not file.is_file() or file.is_symlink():
                raise RuntimeError(f"{file}: not a regular file")
            tar.add(file, arcname=file.name, recursive=False, filter=normalize)
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        if not (len(members) == 10 and all(m.isfile() and "/" not in m.name for m in members)):
            raise RuntimeError(f"{archive}: the contents are not 10 regular files")
    if (system, arch) == (host_os, host_arch):
        got = subprocess.check_output([str(stage / "shikii"), "--version"], text=True).strip()
        if got != "shikii " + version:
            raise RuntimeError(f"{name}: --version says {got!r}")
    print(name + ": checked the build source and the bundled files", flush=True)
    return archive


with ThreadPoolExecutor(max_workers=2) as pool:
    archives = list(pool.map(build, TARGETS))
(assets / "SHA256SUMS").write_text("".join(hashlib.sha256(f.read_bytes()).hexdigest() + "  " + f.name + "\n" for f in sorted(archives)))
shutil.rmtree(work)
print(f"{version} ({commit[:7]}): verified 4 archives, 8 binaries and SHA256SUMS → {assets}", flush=True)
