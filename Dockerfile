# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY . .
ARG BIN=gateway
RUN CGO_ENABLED=0 go build -trimpath -o app ./cmd/${BIN}

# 控制台静态资源：构建进最终镜像，compose/K8s 不再依赖宿主机上的 web/dist。
FROM node:22-alpine AS web
WORKDIR /web
# 不用 corepack：它在部分网络下会因签名校验/下载失败直接中止构建。
# 锁定与本地一致的 pnpm 版本，保证 lockfile 行为可复现。
RUN npm install -g pnpm@11.21.0
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 运行时：极简 alpine，只放二进制 + CA 证书
FROM alpine:3.22

# HTTPS 出站（代理 https 上游、AI 网关调 DeepSeek/Zhipu）需要的根证书，
# 从 golang 阶段拷贝，避免 apk 联网安装
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# 非 root 用户（busybox 自带 addgroup/adduser，无需联网安装）
RUN addgroup -S app && adduser -S -G app app

COPY --from=build /src/app /usr/local/bin/app
# 控制台页面与 admin API 同源，无需单独的 nginx。
COPY --from=web /web/dist /app/web

USER app
EXPOSE 8090 9090 8092
ENTRYPOINT ["app"]
