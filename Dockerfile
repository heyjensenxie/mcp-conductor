# MCP Conductor 多阶段构建：前端 → Go 二进制（内嵌前端） → 最小运行镜像。
#
# GOPROXY 可被构建参数覆盖，例如国内环境：
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct .

# ---- 阶段 1：构建前端 ----
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
# 使用锁文件进行干净、可复现的生产构建；依赖清单变化时立即失败。
RUN npm ci
COPY web/ .
RUN npm run build

# ---- 阶段 2：编译 Go 后端（含内嵌前端产物） ----
FROM golang:1.27-alpine AS builder
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 前端以 node 阶段产物为权威来源（覆盖宿主可能带入的旧 web/dist）。
COPY --from=web /src/web/dist ./web/dist
# 内嵌前端产物到 go:embed 目录（先清空占位 dist）。
RUN rm -rf internal/console/dist && mkdir -p internal/console/dist && cp -r web/dist/. internal/console/dist/
RUN go build -trimpath -ldflags="-s -w" -o /out/mcp-conductor ./cmd/conductor

# ---- 阶段 3：最小运行态 ----
FROM alpine:3.20
RUN adduser -D -u 10001 conductor
WORKDIR /app
COPY --from=builder /out/mcp-conductor /app/mcp-conductor
COPY config.example.yaml /app/config.example.yaml
USER conductor
EXPOSE 8080
ENTRYPOINT ["/app/mcp-conductor"]
