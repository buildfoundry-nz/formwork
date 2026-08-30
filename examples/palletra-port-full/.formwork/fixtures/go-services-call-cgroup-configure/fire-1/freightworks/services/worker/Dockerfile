FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN go build -o /app ./cmd/worker
