FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION
ARG BUILD_TIME
ARG GIT_COMMIT

RUN test -n "$VERSION" \
    && test "$VERSION" != dev \
    && test -n "$BUILD_TIME" \
    && test "$BUILD_TIME" != unknown \
    && printf '%s' "$GIT_COMMIT" | grep -Eq '^[0-9a-f]{40}(-dirty)?$'

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT} -s -w" \
    -o /out/vdoc .

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

ARG VERSION
ARG BUILD_TIME
ARG GIT_COMMIT

LABEL org.opencontainers.image.title="Vdoc backend" \
    org.opencontainers.image.source="https://github.com/ChnMig/Vdoc" \
    org.opencontainers.image.version="$VERSION" \
    org.opencontainers.image.created="$BUILD_TIME" \
    org.opencontainers.image.revision="$GIT_COMMIT"

RUN apk add --no-cache ca-certificates \
    && addgroup -S vdoc \
    && adduser -S -G vdoc vdoc

WORKDIR /app

COPY --from=builder /out/vdoc /app/vdoc
COPY --chown=vdoc:vdoc static ./static

RUN mkdir -p /app/log && chown -R vdoc:vdoc /app

ENV model=release \
    VDOC_SERVER_HOST=0.0.0.0 \
    VDOC_SERVER_PORT=8080 \
    VDOC_SERVER_PID_FILE="" \
    VDOC_DATABASE_ENABLED=false \
    VDOC_STORAGE_ENABLED=false

EXPOSE 8080

USER vdoc

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -q -O - "http://127.0.0.1:${VDOC_SERVER_PORT}/api/v1/open/health" | grep -q '"healthy":true'

ENTRYPOINT ["/app/vdoc"]
