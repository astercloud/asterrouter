# syntax=docker/dockerfile:1

ARG NODE_IMAGE=node:24-alpine
ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:3.23

FROM ${NODE_IMAGE} AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM ${GO_IMAGE} AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG ASTER_VERSION=
ARG ASTER_COMMIT=unknown
ARG ASTER_DATE=unknown
ARG ASTER_BUILD_TYPE=release
RUN VERSION_VALUE="${ASTER_VERSION}" && \
    if [ -z "${VERSION_VALUE}" ] && [ -f ./cmd/asterrouter/VERSION ]; then \
      VERSION_VALUE="$(tr -d '\r\n' < ./cmd/asterrouter/VERSION)"; \
    fi && \
    VERSION_VALUE="${VERSION_VALUE:-0.1.0-dev}" && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Version=${VERSION_VALUE} -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Commit=${ASTER_COMMIT} -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Date=${ASTER_DATE} -X github.com/astercloud/asterrouter/backend/internal/buildinfo.BuildType=${ASTER_BUILD_TYPE}" \
    -o /out/asterrouter ./cmd/asterrouter

FROM ${RUNTIME_IMAGE}
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S -g 10001 asterrouter \
    && adduser -S -D -H -u 10001 -G asterrouter asterrouter \
    && mkdir -p /var/lib/asterrouter/plugin-cache \
        /var/lib/asterrouter/plugin-active \
        /var/lib/asterrouter/artifacts \
        /var/lib/asterrouter/backups \
        /var/lib/asterrouter/diagnostics \
    && chown -R asterrouter:asterrouter /var/lib/asterrouter
COPY --from=backend /out/asterrouter /app/asterrouter
COPY --from=frontend /src/frontend/dist /app/frontend/dist
ENV ASTERROUTER_SERVER_HTTP_LISTEN=:8080 \
    ASTERROUTER_SERVER_HTTP_FRONTEND_DIR=/app/frontend/dist \
    ASTERROUTER_SERVER_PLUGINS_CACHE_DIR=/var/lib/asterrouter/plugin-cache \
    ASTERROUTER_SERVER_PLUGINS_ACTIVE_DIR=/var/lib/asterrouter/plugin-active \
    ASTERROUTER_SERVER_ARTIFACTS_LOCAL_ROOT=/var/lib/asterrouter/artifacts \
    ASTERROUTER_SERVER_MAINTENANCE_BACKUP_DIR=/var/lib/asterrouter/backups \
    ASTERROUTER_SERVER_MAINTENANCE_DIAGNOSTIC_DIR=/var/lib/asterrouter/diagnostics
VOLUME ["/var/lib/asterrouter"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=5 \
    CMD wget -q -T 5 -O /dev/null http://127.0.0.1:8080/ready || exit 1
USER asterrouter
ENTRYPOINT ["/app/asterrouter"]
CMD ["server"]
