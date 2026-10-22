# shikii の画像: shikii と shikii-readout だけ(モデルは別の容れ物。docker-compose.yml を見る)。
# 使い方: docker compose up → 答え合わせの画面 http://127.0.0.1:8095
# 3 段: (a) node で画面(web/)を組む → (b) Go で実行ファイルを組む(画面を埋め込む) → (c) distroless に実行ファイルだけ置く。
# Docker で使う人は Node も Go も要らない。
FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 画面は (a) で組んだものを使う(.dockerignore で web/dist は入ってこない)
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shikii ./cmd/shikii \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shikii-readout ./cmd/shikii-readout

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.licenses="Apache-2.0 OR MIT"
COPY --from=build /out/shikii /out/shikii-readout /usr/local/bin/
COPY --from=build /src/LICENSE /src/LICENSE-APACHE /src/LICENSE-MIT /src/THIRD-PARTY-NOTICES /usr/share/licenses/shikii/
# 基本構成: 実行する場所(/work)の .shikii/ の下に decisions/ wiki/ records/ labels/。ホストのプロジェクトをここに mount する
WORKDIR /work
EXPOSE 8095
ENTRYPOINT ["shikii"]
CMD ["serve", "--store", ".shikii", "--local", "http://readout:8080", "--port", "8095"]
