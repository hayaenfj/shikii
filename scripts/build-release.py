#!/usr/bin/env python3
"""shikii の配布物を組む。リポジトリのルートで、作業ツリーに未コミットの変更が無い状態で実行する。

  python3 -I scripts/build-release.py <出力フォルダ>

版は version.go の Version、ビルド元は HEAD のコミット。<出力フォルダ>/assets/ に 4 環境の tar.gz と
SHA256SUMS を置き、go version -m の結果を <出力フォルダ>/<名前>-<実行ファイル>-build.txt に残す。
"""
import gzip, hashlib, json, os, re, shutil, subprocess, sys, tarfile, tempfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

if len(sys.argv) != 2:
    sys.exit("使い方: python3 -I scripts/build-release.py <出力フォルダ>")
source = Path.cwd()
out = Path(sys.argv[1]).resolve()
commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
if subprocess.check_output(["git", "status", "--porcelain"], text=True):
    sys.exit("作業ツリーに未コミットの変更がある。先にコミットしてから組む")
if out.exists() and any(out.iterdir()):
    sys.exit(f"出力先 {out} が空ではない。前の版の残りを配らないよう、空のフォルダを渡す")
# 無視されたファイル(.DS_Store など)が埋め込みに入らないよう、HEAD を一時フォルダへ複製して組む
work = Path(tempfile.mkdtemp(prefix="shikii-release-"))
subprocess.run(["git", "clone", "-q", "--no-local", str(source), str(work / "src")], check=True)
subprocess.run(["git", "-C", str(work / "src"), "checkout", "-q", "--detach", commit], check=True)
repo = work / "src"
os.chdir(repo)
version = re.search(r'const Version = "([^"]+)"', (repo / "version.go").read_text(encoding="utf-8")).group(1)
problems = []
# 組む Go の版は go.mod の toolchain(脆弱性の直った版)。GOTOOLCHAIN=local などで別の版で組んだら止める(監査 0.8.2-2 の B1)
toolchain = re.search(r"^toolchain (go\S+)$", (repo / "go.mod").read_text(encoding="utf-8"), re.M)
if not toolchain:
    problems.append("go.mod に toolchain の行が無い")
if json.loads((repo / ".claude-plugin/plugin.json").read_text(encoding="utf-8")).get("version") != version:
    problems.append(".claude-plugin/plugin.json の version が " + version + " ではない")
for f in ["README.md", "docs/readme/README_ja.md"]:
    for v in re.findall(r"badge/version-([0-9.]+)-", (repo / f).read_text(encoding="utf-8")):
        if v != version:
            problems.append(f"{f}: 版のバッジが {v}")
for f in ["CHANGELOG.md", "CHANGELOG_ja.md"]:
    head = next(l for l in (repo / f).read_text(encoding="utf-8").splitlines() if l.startswith("## "))
    if not head.startswith(f"## {version} — ") or re.search(r"unreleased|未公開", head, re.I):
        problems.append(f"{f}: 先頭の見出しが公開の形ではない: {head}")
tag = subprocess.run(["git", "-C", str(source), "rev-parse", "-q", "--verify", f"refs/tags/v{version}^{{commit}}"], capture_output=True, text=True).stdout.strip()
if tag and tag != commit:
    problems.append(f"タグ v{version} は {tag[:7]} を指し、HEAD {commit[:7]} と違う")
if problems:
    shutil.rmtree(work)
    sys.exit("公開の形が揃っていない:\n  " + "\n  ".join(problems))
vcs_time = int(subprocess.check_output(["git", "show", "-s", "--format=%ct", commit], text=True))
out.mkdir(parents=True, exist_ok=True)
assets = out / "assets"
assets.mkdir(exist_ok=True)
install = f"""shikii {version} の導入・更新

注意: 0.6.0 以前からの更新は、初期化済みの各プロジェクトの .shikii/ の中を全部消して初期化し直します。
以前の会話・教訓・正解・較正は消えます。残したいものは先に控えてください。

既に shikii update が使える場合:
  shikii update

手動で配置する場合は、同梱の shikii と shikii-readout をPATHの通った同じフォルダへ置きます。
SHA256SUMSでアーカイブのハッシュを確かめ、使用中の実行ファイルへ直接上書きせず、別名で用意してから mv で置き換えてください（同じファイルに上書きすると、macOS では以後の起動が Killed: 9 になります）。
ライセンス4ファイルは実行ファイルの隣の shikii-licenses/ に保管します。
続けて次を実行します:
  shikii hook install --yes

利用するプロジェクトだけで一度初期化します:
  shikii --init

更新後はエージェントの新しいセッションを始め、起動中の画面・MCPを再起動します。
今回のskillとhookを更新します。
推奨のモデルは TypeSafe の Jev（環境変数 TYPESAFE_API_KEY）。鍵を置けない時は llama.cpp。README_ja.mdの「必要なもの」を参照してください。
shikii本体のライセンスはApache-2.0 OR MITです。第三者の条件はTHIRD-PARTY-NOTICESを参照してください。
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
            raise RuntimeError(f"{name}/{binary}: ビルド元の情報が合わない\n{info}")
        if not info.splitlines()[0].endswith(": " + toolchain.group(1)):
            raise RuntimeError(f"{name}/{binary}: {toolchain.group(1)} で組まれていない(go.mod の toolchain)\n{info.splitlines()[0]}")
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
                raise RuntimeError(f"{file}: 通常のファイルではない")
            tar.add(file, arcname=file.name, recursive=False, filter=normalize)
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        if not (len(members) == 10 and all(m.isfile() and "/" not in m.name for m in members)):
            raise RuntimeError(f"{archive}: 中身が 10 個の通常のファイルではない")
    if (system, arch) == (host_os, host_arch):
        got = subprocess.check_output([str(stage / "shikii"), "--version"], text=True).strip()
        if got != "shikii " + version:
            raise RuntimeError(f"{name}: --version が {got!r}")
    print(name + ": ビルド元と同梱物を確認", flush=True)
    return archive


with ThreadPoolExecutor(max_workers=2) as pool:
    archives = list(pool.map(build, TARGETS))
(assets / "SHA256SUMS").write_text("".join(hashlib.sha256(f.read_bytes()).hexdigest() + "  " + f.name + "\n" for f in sorted(archives)))
shutil.rmtree(work)
print(f"{version} ({commit[:7]}): 4配布物・8実行ファイル・SHA256SUMSを検証済み → {assets}", flush=True)
