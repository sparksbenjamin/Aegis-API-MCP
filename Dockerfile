# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

RUN addgroup -S aegis && adduser -S aegis -G aegis
WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download && go mod verify

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
    -ldflags="-w -s -extldflags=-static" \
    -trimpath \
    -o /out/aegis-api-mcp \
    .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/aegis-api-mcp /usr/local/bin/aegis-api-mcp
COPY --from=builder --chown=65532:65532 /build/configs /configs

ENV AEGIS_CONFIGS_DIR=/configs

USER nonroot

ENTRYPOINT ["/usr/local/bin/aegis-api-mcp"]
