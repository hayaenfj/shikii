**English** · [日本語](docker.ja.md)

# Running the business API and page with Docker or Podman

The container entry point is for the business decision API and the answer page. Installing the hooks into Claude Code or Codex on the host is done with [the usual setup](../README.md#get-started).

## What you need

| Setup | What it needs |
|---|---|
| Docker | a running Docker Engine and Docker Compose, and a clone of the repository |
| Podman | a running Podman engine and an external Compose provider, and a clone of the repository |
| When building the image | a network connection. The build containers provide Node.js 24 and Go 1.27 |
| When running the decision model for the first time | a connection to download the model and space to store it. The default is Ternary-Bonsai-27B Q2_g64 (about 7.6GB. An ordinary chat model; shikii-readout answers in the decision model's shape) |

[About Docker Compose](https://docs.docker.com/compose/). Podman's `compose` calls an external provider such as docker-compose or podman-compose, so a provider is needed besides Podman itself. [Podman's official description](https://docs.podman.io/en/latest/markdown/podman-compose.1.html).

## First-time setup and start

Clone the repository and, in the shikii folder, first build the image and prepare one business question.

```sh
docker compose build shikii
docker compose run --rm --no-deps shikii new same-issue --from decisions/same-issue.yaml
docker compose up
# → http://127.0.0.1:8095
```

In a project that already has questions in `.shikii/decisions/`, start from `up`. `new` does not overwrite an existing question of the same name. When copying a sample, the sample's old line is not brought in.

On Linux, to match the owner of the bind mount, put `env UID="$(id -u)" GID="$(id -g)"` in front of each compose command. The Compose file's `user` uses those values. Do not assign to the shell's readonly UID variable; pass the values to the child process.

With Podman, set up a provider and use the same `docker-compose.yml` through `podman compose`. Starting the whole set of Compose plus the model was not measured in this update. Check it, including the provider and the permissions of the host-side volumes.

## Where things are stored, and the model

- Mount the project at `/work` so the questions, knowledge, records and truths in `.shikii/` stay on the host. Recreating the container uses the same material.
- Compose starts llama.cpp's official server, shikii-readout and shikii. llama.cpp listens on `0.0.0.0:8086`, readout waits for `http://llama:8086` to be ready and answers on `8080`, and shikii waits for `http://readout:8080` to be ready (when `TYPESAFE_API_KEY` is passed, it starts without waiting). The page is served on the host at `127.0.0.1:8095`. If you switch to a trained decision model such as OpenJev, readout passes its answers through unchanged, so the endpoint stays the same. Kev-4B is not supported.
- shikii tells it is in a container from Docker's `/.dockerenv`, Podman's `/run/.containerenv` and similar. When you change the model or the runtime, check where that setup listens and connects.
- To use the recommended TypeSafe Jev, pass `TYPESAFE_API_KEY` to the shikii container as an environment variable. With the key, decisions with `pii: false` are sent to Jev, and llama and readout are used only for decisions with `pii: true`. Also check that the decisions you use allow sending outside.
- To use an ordinary chat GGUF, put `shikii-readout` between it and llama.cpp. [Conditions and examples for the converter](readout.md). It is not a converter that connects directly to any chat API.

HTTP decisions go to `POST /v1/decide`, and truths to `POST /v1/label`. A JSON Content-Type is required. [The list of APIs](../docs/guide.md#the-serve-http-api).

## What is in the image, and licenses

The `Dockerfile` builds the page with Node.js, builds the two executables with Go, and puts them in a distroless image for running. If you only use it on the host, you need neither Go nor Node.js. It does not run as root.

shikii itself is `Apache-2.0 OR MIT`. The OCI `org.opencontainers.image.licenses` carries the same expression, and `LICENSE`, `LICENSE-APACHE`, `LICENSE-MIT` and `THIRD-PARTY-NOTICES` go into `/usr/share/licenses/shikii/`. Dependencies, the base image and the model follow their own terms.
