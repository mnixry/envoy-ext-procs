FROM golang:1.25-trixie AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=go-mod,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,id=go-build,target=/root/.cache/go-build \
    --mount=type=cache,id=go-mod,target=/go/pkg/mod \
    mkdir -p /out && \
    for pkg in ./cmd/*; do go build -o /out/$(basename $pkg) -ldflags "-w -s" -v $pkg; done && \
    ls -la /out

FROM debian:trixie-slim AS runner

RUN apt-get update && \
    apt-get install -y tini ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/* /usr/local/bin/
EXPOSE 9002
ENTRYPOINT ["/usr/bin/tini", "--"]