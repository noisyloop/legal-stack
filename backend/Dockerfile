# Multi-stage build: full toolchain to compile, distroless to run.
# The runtime image contains the binary and nothing else — no shell, no
# package manager, no root user. Its attack surface is the binary itself.

FROM golang:1.24 AS builder
WORKDIR /src

# Dependency layer first so code changes don't re-download modules.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]
