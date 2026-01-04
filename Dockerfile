FROM golang:1.25-trixie AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=go-mod,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,id=go-build,target=/root/.cache/go-build \
    --mount=type=cache,id=go-mod,target=/go/pkg/mod \
    make -j$(nproc) build

FROM debian:trixie-slim AS runner

RUN apt-get update && \
    apt-get install -y tini ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/bin/* /usr/local/bin/
EXPOSE 9002
ENTRYPOINT ["/usr/bin/tini", "--"]