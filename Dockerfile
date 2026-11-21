FROM node:26.13.1-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS web
RUN apk add --no-cache --upgrade ca-certificates-bundle=20261020-r0
ARG CLICKCLACK_WEB_VERSION=dev
ENV CLICKCLACK_WEB_VERSION=$CLICKCLACK_WEB_VERSION
WORKDIR /src
RUN npm install -g pnpm@12.15.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/desktop/package.json apps/desktop/package.json
COPY apps/web/package.json apps/web/package.json
COPY examples/bot-ts/package.json examples/bot-ts/package.json
COPY packages/protocol/package.json packages/protocol/package.json
COPY packages/sdk-ts/package.json packages/sdk-ts/package.json
RUN pnpm install --frozen-lockfile
COPY apps apps
COPY packages packages
COPY scripts scripts
RUN pnpm build

FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS api
RUN apk add --no-cache --upgrade ca-certificates=20261020-r0 ca-certificates-bundle=20261020-r0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/api apps/api
COPY infra infra
COPY --from=web /src/apps/api/internal/webassets/dist apps/api/internal/webassets/dist
RUN go build -o /out/clickclack ./apps/api/cmd/clickclack

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache --upgrade ca-certificates-bundle=20261020-r0
RUN adduser -D -H clickclack
WORKDIR /app
COPY --from=api /out/clickclack /usr/local/bin/clickclack
RUN mkdir -p /app/data && chown -R clickclack:clickclack /app
USER clickclack
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["clickclack"]
CMD ["serve", "--addr", ":8080", "--data", "/app/data"]
