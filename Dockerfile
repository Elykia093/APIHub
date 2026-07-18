# syntax=docker/dockerfile:1.7

FROM node:24-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d AS web-build

WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN --mount=type=cache,target=/root/.npm \
    cd web && npm ci
COPY web ./web
RUN cd web && npm run build

FROM golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS go-build

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG REVISION=unknown

WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY server ./
RUN rm -rf /src/server/internal/webui/dist
COPY --from=web-build /src/server/internal/webui/dist ./internal/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath \
    -ldflags="-s -w -buildid= -X main.version=$VERSION -X main.revision=$REVISION" \
    -o /out/apihub ./cmd/apihub

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b AS runtime

ARG VERSION=dev
ARG REVISION=unknown

LABEL org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$REVISION"

ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=4180 \
    GOMEMLIMIT=96MiB

COPY --from=go-build --chown=nonroot:nonroot /out/apihub /apihub

USER nonroot:nonroot
EXPOSE 4180
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/apihub", "healthcheck"]

ENTRYPOINT ["/apihub"]
