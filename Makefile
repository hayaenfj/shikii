# shikii: 決定モデルの運用フレームワーク(コマンド名も shikii)
BIN     ?= bin/shikii
INSTALL ?= $(HOME)/.local/bin/shikii
STORE   ?= $(HOME)/.claude/jev/decisions-store
MODEL   ?= ggml-org/Kev-4B-GGUF:Q4_K_M

.PHONY: build test install check status drift server demo fake

build:
	go build -o $(BIN) ./cmd/shikii
	go build -o bin/fake-systemone ./cmd/fake-systemone
	go build -o bin/shikii-readout ./cmd/readout

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
	go vet ./... && go test ./... && scripts/check-readme.sh

install: build
	cp $(BIN) $(INSTALL)

# 契約の検査(CI と同じ store で。違反で exit 1)。較正を書き直したら testdata/store/labels/ も写し直す
check: build
	$(BIN) check --store testdata/store decisions/*.yaml

# claude-jev の hook が溜めたラベルの棚卸し
status: build
	$(BIN) status --store $(STORE) decisions/prompt-kind.yaml

drift: build
	$(BIN) drift decisions/prompt-kind.yaml --model '$(MODEL)' --store $(STORE)

# 1 プロセスで HTTP の口と画面(http://127.0.0.1:8095/)。LOCAL= で手元のモデル、STORE= で置き場
LOCAL ?= http://127.0.0.1:8080
serve: build
	$(BIN) serve --specs decisions --store $(STORE) --local $(LOCAL)

# 古くてラベルの無い記録の掃除。表示だけ。消すなら make gc GC_FLAGS=--apply
gc: build
	$(BIN) gc --store $(STORE) $(GC_FLAGS)

# ローカルの決定モデル(初回は Kev-4B 約 2.5GB を取得)
server:
	llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M --port 8080 --host 127.0.0.1
