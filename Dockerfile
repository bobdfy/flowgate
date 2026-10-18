# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY . .
ARG BIN=gateway
RUN CGO_ENABLED=0 go build -trimpath -o app ./cmd/${BIN}

# 运行时：极简 alpine，只放二进制 + CA 证书
FROM alpine:3.22

# HTTPS 出站（代理 https 上游、AI 网关调 DeepSeek/Zhipu）需要的根证书，
# 从 golang 阶段拷贝，避免 apk 联网安装
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# 非 root 用户（busybox 自带 addgroup/adduser，无需联网安装）
RUN addgroup -S app && adduser -S -G app app

COPY --from=build /src/app /usr/local/bin/app

USER app
EXPOSE 8090 9090 8092
ENTRYPOINT ["app"]
