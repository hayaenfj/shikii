#!/usr/bin/env python3
"""shikii の配布物を組む。リポジトリのルートで、作業ツリーに未コミットの変更が無い状態で実行する。

  python3 -I scripts/build-release.py <出力フォルダ>

版は version.go の Version、ビルド元は HEAD のコミット。<出力フォルダ>/assets/ に 4 環境の tar.gz と
SHA256SUMS を置き、go version -m の結果を <出力フォルダ>/<名前>-<実行ファイル>-build.txt に残す。
"""
import hashlib, os, re, shutil, subprocess, sys, tarfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

if len(sys.argv) != 2:
    sys.exit("使い方: python3 -I scripts/build-release.py <出力フォルダ>")
repo = Path.cwd()
out = Path(sys.argv[1]).resolve()
version = re.search(r'const Version = "([^"]+)"', (repo / "version.go").read_text(encoding="utf-8")).group(1)
commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
if subprocess.check_output(["git", "status", "--porcelain"], text=True):
    sys.exit("作業ツリーに未コミットの変更がある。先にコミットしてから組む")
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
        assert "vcs.revision=" + commit in info and "vcs.modified=false" in info
        assert "GOOS=" + system in info and "GOARCH=" + arch in info
        (out / (name + "-" + binary + "-build.txt")).write_text(info)
    for source, dest in BUNDLED:
        shutil.copy2(repo / source, stage / dest)
    (stage / "INSTALL.txt").write_text(install)
    archive = assets / (name + ".tar.gz")
    with tarfile.open(archive, "w:gz") as tar:
        for file in sorted(stage.iterdir()):
            assert file.is_file() and not file.is_symlink()
            tar.add(file, arcname=file.name, recursive=False)
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        assert len(members) == 10 and all(m.isfile() and "/" not in m.name for m in members)
    print(name + ": ビルド元と同梱物を確認", flush=True)
    return archive


with ThreadPoolExecutor(max_workers=2) as pool:
    archives = list(pool.map(build, TARGETS))
(assets / "SHA256SUMS").write_text("".join(hashlib.sha256(f.read_bytes()).hexdigest() + "  " + f.name + "\n" for f in sorted(archives)))
print(f"{version} ({commit[:7]}): 4配布物・8実行ファイル・SHA256SUMSを検証済み → {assets}", flush=True)
