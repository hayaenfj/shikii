# shikii: 決定モデルの運用フレームワーク(コマンド名も shikii)
BIN     ?= bin/shikii
INSTALL ?= $(HOME)/.local/bin/shikii
STORE   ?= .shikii
# drift の鍵は応答の model(shikii status の行に出る名)。モデルを替えたら合わせる
MODEL   ?= jev-1.14.0

.PHONY: build test install check status drift server demo fake web

# 画面(web/、Svelte + Tailwind CSS)のビルド。結果 web/dist は実行ファイルに埋め込まれ、repo にも commit する(使うだけなら Node は要らない)。
# 画面を直したら make web → commit。Node 24 と npm が要る
web:
	cd web && npm ci && npm run build

# web/dist が無い、または元(web/src など)より古ければ組む(監査 7 の 9)。commit 済みの dist を埋め込む
web/dist/index.html: $(shell find web/src web/index.html web/vite.config.js web/package-lock.json -type f 2>/dev/null)
	$(MAKE) web

build: web/dist/index.html
	go build -o $(BIN) ./cmd/shikii
	go build -o bin/fake-systemone ./cmd/fake-systemone
	go build -o bin/shikii-readout ./cmd/shikii-readout

# 普通の GGUF を System One にするアダプタ(後ろに llama-server が要る。UPSTREAM で指定)
UPSTREAM ?= http://127.0.0.1:8086
readout: build
	bin/shikii-readout --upstream $(UPSTREAM) --port 8090

# アダプタの後ろに置く llama-server(本家でも fork でも同じフラグ)。GGUF= と ALIAS= を渡す。
#   -fa on: flash attention  -ctk/-ctv q8_0: KV キャッシュを 8bit に  -np 1: スロットを割らず文脈を全部 1 本に
#   アダプタは state を最後に置くので、同じ spec の 2 問目以降は cache_prompt で接頭辞が再利用される
GGUF  ?=
ALIAS ?= gguf
server-gguf:
	llama-server -m "$(GGUF)" --port 8086 --host 127.0.0.1 -c 4096 -ngl 99 -fa on -ctk q8_0 -ctv q8_0 -np 1 --alias $(ALIAS)

# 鍵も GPU も無しで輪を一周(偽モデル)
demo:
	scripts/demo.sh

# 偽モデルだけ起こす(:8099)
fake: build
	bin/fake-systemone --port 8099

test:
	test -z "$$(gofmt -l .)" || (gofmt -l . && echo 'gofmt: 上のファイルを整形して' && exit 1)
	go vet ./... && scripts/go-test.sh && scripts/check-readme.sh && scripts/check-skill.sh

# 消してから置く(新しい inode)。使用中の実行ファイルを cp で上書きすると macOS が署名を無効にして以後 SIGKILL(exit 137)する(hook が全部死ぬ、2026-10-18)
install: build
	rm -f $(INSTALL) $(dir $(INSTALL))shikii-readout
	cp $(BIN) $(INSTALL)
	cp bin/shikii-readout $(dir $(INSTALL))shikii-readout

# 契約の検査(CI と同じ store で。違反で exit 1)。較正を書き直したら testdata/store/labels/ も写し直す
check: build
	$(BIN) check --store testdata/store decisions/*.yaml

# AI 自身の判断(shikii hook)が溜めた記録の棚卸し
status: build
	$(BIN) status --store $(STORE) $(STORE)/decisions/*.yaml

drift: build
	$(BIN) drift $(STORE)/decisions/prompt-kind.yaml --model '$(MODEL)' --store $(STORE)

# 1 プロセスで HTTP の口と画面(http://127.0.0.1:8095/)。LOCAL= で手元のモデル、STORE= で置き場
LOCAL ?= http://127.0.0.1:8080
# この repo の開発画面は見本の spec を明示する。個別の .shikii/ を共有先へリンクしない。
serve: build
	$(BIN) serve --specs decisions --store $(STORE) --local $(LOCAL)

# 古くてラベルの無い記録の掃除。表示だけ。消すなら make gc GC_FLAGS=--apply
gc: build
	$(BIN) gc --store $(STORE) $(GC_FLAGS)

# 手元の標準の決定モデル(shikii up: llama-server :8086 と shikii-readout :8080。初回は Ternary-Bonsai-27B 約 7.6GB を取得)。
# TYPESAFE_API_KEY があると up は手元のモデルを起こさず断る(Jev を使う)。鍵を置いたまま起こすなら SHIKII_MODEL=local make server
server:
	go build -o $(BIN) ./cmd/shikii
	$(BIN) up
