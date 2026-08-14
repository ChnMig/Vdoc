FROM golang:1.25.5-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT} -s -w" \
    -o /out/vdoc .

FROM alpine:3.23

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
    VDOC_DATABASE_ENABLED=false \
    VDOC_STORAGE_ENABLED=false

EXPOSE 8080

USER vdoc

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -q -O - "http://127.0.0.1:${VDOC_SERVER_PORT}/api/v1/open/health" | grep -q '"healthy":true'

ENTRYPOINT ["/app/vdoc"]
