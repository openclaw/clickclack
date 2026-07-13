FROM node:24-alpine@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS web
ARG CLICKCLACK_VERSION=0.0.0-local
ARG CLICKCLACK_COMMIT=unknown
ARG CLICKCLACK_BUILD_DATE=1970-01-01T00:00:00Z
ARG CLICKCLACK_WEB_VERSION
WORKDIR /src
RUN npm install -g pnpm@11.9.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
COPY packages/protocol/package.json packages/protocol/package.json
COPY packages/sdk-ts/package.json packages/sdk-ts/package.json
RUN pnpm install --frozen-lockfile
COPY apps apps
COPY packages packages
COPY scripts scripts
RUN test -n "$CLICKCLACK_VERSION" \
    && test -n "$CLICKCLACK_COMMIT" \
    && test -n "$CLICKCLACK_BUILD_DATE" \
    && CLICKCLACK_WEB_VERSION="${CLICKCLACK_WEB_VERSION:-$CLICKCLACK_VERSION}" pnpm build

FROM golang:1.26-alpine@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS api
ARG CLICKCLACK_VERSION=0.0.0-local
ARG CLICKCLACK_COMMIT=unknown
ARG CLICKCLACK_BUILD_DATE=1970-01-01T00:00:00Z
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/api apps/api
COPY infra infra
COPY --from=web /src/apps/api/internal/webassets/dist apps/api/internal/webassets/dist
RUN test -n "$CLICKCLACK_VERSION" \
    && test -n "$CLICKCLACK_COMMIT" \
    && test -n "$CLICKCLACK_BUILD_DATE" \
    && go build -trimpath \
      -ldflags "-s -w -X main.version=$CLICKCLACK_VERSION -X main.commit=$CLICKCLACK_COMMIT -X main.date=$CLICKCLACK_BUILD_DATE" \
      -o /out/clickclack ./apps/api/cmd/clickclack \
    && test "$(/out/clickclack version)" = \
      "clickclack $CLICKCLACK_VERSION ($CLICKCLACK_COMMIT, $CLICKCLACK_BUILD_DATE)"

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
ARG CLICKCLACK_VERSION=0.0.0-local
ARG CLICKCLACK_COMMIT=unknown
ARG CLICKCLACK_BUILD_DATE=1970-01-01T00:00:00Z
LABEL org.opencontainers.image.version="$CLICKCLACK_VERSION" \
      org.opencontainers.image.revision="$CLICKCLACK_COMMIT" \
      org.opencontainers.image.created="$CLICKCLACK_BUILD_DATE"
RUN adduser -D -H clickclack
WORKDIR /app
COPY --from=api /out/clickclack /usr/local/bin/clickclack
RUN mkdir -p /app/data && chown -R clickclack:clickclack /app
USER clickclack
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["clickclack"]
CMD ["serve", "--addr", ":8080", "--data", "/app/data"]
