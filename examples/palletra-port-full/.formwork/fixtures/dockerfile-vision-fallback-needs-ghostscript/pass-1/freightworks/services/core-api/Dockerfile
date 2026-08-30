FROM golang:1.25 AS builder
WORKDIR /build
COPY freightworks/ .
RUN CGO_ENABLED=1 GOOS=linux go build -o svc ./services/core-api/

FROM docker.io/library/debian:bookworm-slim
# Raster path: `gm convert` turns drawing PDFs into PNGs; both packages required.
RUN apt-get update \
 && apt-get install -y --no-install-recommends graphicsmagick ghostscript \
      libfontconfig1 ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/svc /usr/local/bin/svc
EXPOSE 8090
CMD ["/usr/local/bin/svc"]
