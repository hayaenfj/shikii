# shikii の画像: shikii と shikii-readout だけ(モデルは別の容れ物。docker-compose.yml を見る)。
# 使い方: docker compose up → 答え合わせの画面 http://127.0.0.1:8095
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shikii ./cmd/shikii \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shikii-readout ./cmd/readout

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/shikii /out/shikii-readout /usr/local/bin/
# 基本構成: 実行する場所(/work)に decisions/ wiki/ .shikii/。ホストのプロジェクトをここに mount する
WORKDIR /work
EXPOSE 8095
ENTRYPOINT ["shikii"]
CMD ["serve", "--specs", "decisions", "--store", ".shikii", "--local", "http://llama:8080", "--port", "8095"]
