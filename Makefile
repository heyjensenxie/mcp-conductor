# MCP Conductor 常用开发命令。
# Windows 下无 make 时，可照抄对应命令或在 Git Bash 中执行。

BIN := bin/mcp-conductor

.PHONY: build run test vet fmt web-install web-dev web-build dev docker-up docker-down

## 构建后端（本地；前端未构建时使用占位 console）
build:
	mkdir -p bin && go build -o $(BIN) ./cmd/conductor

## 直接运行后端
run:
	go run ./cmd/conductor

## 单元测试
test:
	go test ./...

## 静态检查
vet:
	go vet ./...

## 格式化（仅改动的包由 IDE/CI 校验，此处供整库使用）
fmt:
	gofmt -w ./cmd ./internal

## 前端依赖安装
web-install:
	npm --prefix web install

## 前端开发服务器（代理 /api 与 /mcp 到 :8080）
web-dev:
	npm --prefix web run dev

## 构建前端产物
web-build:
	npm --prefix web run build

## 构建单二进制（先构建前端，嵌入真实 Console 产物）
build-console:
	npm --prefix web run build && rm -rf internal/console/dist && cp -r web/dist internal/console/dist && go build -o $(BIN) ./cmd/conductor

## 数据库/队列基础设施已由 docker compose up 提供
docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

## 应用数据库迁移到 Compose 起的 MySQL 5.7
db-migrate:
	docker compose exec -T mysql mysql -uconductor -pconductor --default-character-set=utf8mb4 conductor < migrations/0001_init_schema.sql

## 本地一键启动后端
dev: run