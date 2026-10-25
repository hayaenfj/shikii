# shikii: a framework for running decision models (the command is also called shikii)
BIN     ?= bin/shikii
INSTALL ?= $(HOME)/.local/bin/shikii
STORE   ?= .shikii
# The drift key is the model in the response (the name shown on the shikii status line). Change it when you switch models
MODEL   ?= jev-1.14.0

.PHONY: build test check-english install check status drift server demo fake web

# Build the page (web/, Svelte + Tailwind CSS). The result web/dist is embedded in the binary and also committed to the repo (Node is not needed just to use it).
# After changing the page, make web → commit. Needs Node 24 and npm
web:
	cd web && npm ci && npm run build

# Build if web/dist is missing or older than the source (web/src etc.) (audit 7 item 9). Embeds the committed dist
web/dist/index.html: $(shell find web/src web/index.html web/vite.config.js web/package-lock.json -type f 2>/dev/null)
	$(MAKE) web

build: web/dist/index.html
	go build -o $(BIN) ./cmd/shikii
	go build -o bin/fake-systemone ./cmd/fake-systemone
	go build -o bin/shikii-readout ./cmd/shikii-readout

# Adapter that turns a plain GGUF into System One (needs llama-server behind it; set it with UPSTREAM)
UPSTREAM ?= http://127.0.0.1:8086
readout: build
	bin/shikii-readout --upstream $(UPSTREAM) --port 8090

# llama-server placed behind the adapter (same flags for upstream and forks). Pass GGUF= and ALIAS=.
#   -fa on: flash attention  -ctk/-ctv q8_0: 8-bit KV cache  -np 1: do not split slots, use the whole context as one
#   The adapter puts state last, so from the 2nd question of the same spec on, cache_prompt reuses the prefix
GGUF  ?=
ALIAS ?= gguf
server-gguf:
	llama-server -m "$(GGUF)" --port 8086 --host 127.0.0.1 -c 4096 -ngl 99 -fa on -ctk q8_0 -ctv q8_0 -np 1 --alias $(ALIAS)

# Go around the loop with no key and no GPU (fake model)
demo:
	scripts/demo.sh

# Start only the fake model (:8099)
fake: build
	bin/fake-systemone --port 8099

test:
	test -z "$$(gofmt -l .)" || (gofmt -l . && echo 'gofmt: format the files above' && exit 1)
	go vet ./... && scripts/go-test.sh && scripts/check-readme.sh && scripts/check-skill.sh

# What ships is in English (development may be written in Japanese first; run this before pushing. CI runs it on every push)
check-english:
	python3 scripts/test-check-english.py && python3 scripts/check-english.py

# Delete, then place (new inode). Overwriting a running binary with cp makes macOS invalidate its signature and SIGKILL it from then on (exit 137) (every hook died, 2026-10-18)
install: build
	rm -f $(INSTALL) $(dir $(INSTALL))shikii-readout
	cp $(BIN) $(INSTALL)
	cp bin/shikii-readout $(dir $(INSTALL))shikii-readout

# Contract check (with the same store as CI; exit 1 on violation). After rewriting a calibration, copy testdata/store/labels/ again too
check: build
	$(BIN) check --store testdata/store decisions/*.yaml

# Inventory of the records collected by the AI's own judgments (shikii hook)
status: build
	$(BIN) status --store $(STORE) $(STORE)/decisions/*.yaml

drift: build
	$(BIN) drift $(STORE)/decisions/prompt-kind.yaml --model '$(MODEL)' --store $(STORE)

# HTTP endpoint and page in one process (http://127.0.0.1:8095/). LOCAL= for a local model, STORE= for the place
LOCAL ?= http://127.0.0.1:8080
# The development page of this repo names the sample specs explicitly. It does not link an individual .shikii/ to a shared place.
serve: build
	$(BIN) serve --specs decisions --store $(STORE) --local $(LOCAL)

# Clean up old records without labels. Display only. To delete, make gc GC_FLAGS=--apply
gc: build
	$(BIN) gc --store $(STORE) $(GC_FLAGS)

# The standard local decision model (shikii up: llama-server :8086 and shikii-readout :8080. The first run downloads Ternary-Bonsai-27B, about 7.6GB).
# With TYPESAFE_API_KEY set, up refuses to start the local model (Jev is used). To start it with the key still set, SHIKII_MODEL=local make server
server:
	go build -o $(BIN) ./cmd/shikii
	$(BIN) up
