FROM golang:1.25 AS builder
WORKDIR /build
COPY freightworks/ .
RUN CGO_ENABLED=1 GOOS=linux go build -o svc ./services/core-api/

FROM docker.io/library/debian:bookworm-slim
# TODO: the runtime image should install graphicsmagick + ghostscript so the
# W3 vision fallback can rasterise scanned PDFs via `gm convert`.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates libfontconfig1 ghostscript && \
    rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/svc /usr/local/bin/svc
EXPOSE 8090
CMD ["/usr/local/bin/svc"]
