# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.5

FROM golang:${GO_VERSION}-bookworm AS source
RUN apt-get update \
    && apt-get install --yes --no-install-recommends libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM source AS api-build
RUN CGO_ENABLED=1 go build \
    -trimpath \
    -ldflags='-s -w' \
    -o /out/download-your-data \
    ./cmd/download-your-data

FROM debian:bookworm-slim AS api
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates libsqlite3-0 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 65532 download-your-data \
    && useradd --uid 65532 --gid 65532 --no-create-home --shell /usr/sbin/nologin download-your-data \
    && install --directory --owner 65532 --group 65532 --mode 0700 /var/lib/download-your-data
COPY --from=api-build /out/download-your-data /usr/local/bin/download-your-data
USER 65532:65532
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/download-your-data", "serve"]

FROM source AS pages-build
RUN CGO_ENABLED=1 go run ./cmd/render-pages \
    --profile /src/configs/production.yml \
    --output /out/pages

FROM scratch AS pages
COPY --from=pages-build /out/pages /
