FROM node:26-alpine@sha256:2d984a15c9b54fd0aeb608b8e0d0d83529eb34d2966db27a1fb4f1edc3d298a3 AS web
ARG CLICKCLACK_WEB_VERSION=dev
ENV CLICKCLACK_WEB_VERSION=$CLICKCLACK_WEB_VERSION
WORKDIR /src
RUN npm install -g pnpm@11.24.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
COPY packages/protocol/package.json packages/protocol/package.json
COPY packages/sdk-ts/package.json packages/sdk-ts/package.json
RUN pnpm install --frozen-lockfile
COPY apps apps
COPY packages packages
COPY scripts scripts
RUN pnpm build

FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/api apps/api
COPY infra infra
COPY --from=web /src/apps/api/internal/webassets/dist apps/api/internal/webassets/dist
RUN go build -o /out/clickclack ./apps/api/cmd/clickclack

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN adduser -D -H clickclack
WORKDIR /app
COPY --from=api /out/clickclack /usr/local/bin/clickclack
RUN mkdir -p /app/data && chown -R clickclack:clickclack /app
USER clickclack
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["clickclack"]
CMD ["serve", "--addr", ":8080", "--data", "/app/data"]
