FROM node:26-alpine@sha256:0b36e8c136b94cd4fcf02188228e76c31ad5872eef3fec8cbd2eee500cfd9e80 AS web
ARG CLICKCLACK_WEB_VERSION=dev
ENV CLICKCLACK_WEB_VERSION=$CLICKCLACK_WEB_VERSION
WORKDIR /src
RUN npm install -g pnpm@12.6.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
COPY packages/protocol/package.json packages/protocol/package.json
COPY packages/sdk-ts/package.json packages/sdk-ts/package.json
RUN pnpm install --frozen-lockfile
COPY apps apps
COPY packages packages
COPY scripts scripts
RUN pnpm build

FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/api apps/api
COPY infra infra
COPY --from=web /src/apps/api/internal/webassets/dist apps/api/internal/webassets/dist
RUN go build -o /out/clickclack ./apps/api/cmd/clickclack

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN adduser -D -H clickclack
WORKDIR /app
COPY --from=api /out/clickclack /usr/local/bin/clickclack
RUN mkdir -p /app/data && chown -R clickclack:clickclack /app
USER clickclack
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["clickclack"]
CMD ["serve", "--addr", ":8080", "--data", "/app/data"]
