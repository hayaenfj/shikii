# The shikii image: only shikii and shikii-readout (the model runs in a separate container; see docker-compose.yml).
# Usage: docker compose up → the answer-checking page at http://127.0.0.1:8095
# 3 stages: (a) build the page (web/) with node → (b) build the binaries with Go (embedding the page) → (c) put only the binaries on distroless.
# People who use Docker need neither Node nor Go.
FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# The official image has GOTOOLCHAIN=local, so go.mod's toolchain has no effect. Pin the version here
FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Use the page built in (a) (.dockerignore keeps web/dist out)
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shikii ./cmd/shikii \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shikii-readout ./cmd/shikii-readout

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.licenses="Apache-2.0 OR MIT"
COPY --from=build /out/shikii /out/shikii-readout /usr/local/bin/
COPY --from=build /src/LICENSE /src/LICENSE-APACHE /src/LICENSE-MIT /src/THIRD-PARTY-NOTICES /usr/share/licenses/shikii/
# Basic layout: decisions/ wiki/ records/ labels/ under .shikii/ in the working place (/work). Mount the host's project here
WORKDIR /work
EXPOSE 8095
ENTRYPOINT ["shikii"]
CMD ["serve", "--store", ".shikii", "--local", "http://readout:8080", "--port", "8095"]
