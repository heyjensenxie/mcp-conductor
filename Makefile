# MCP Conductor 常用开发命令。
# Windows 下无 make 时，可照抄对应命令或在 Git Bash 中执行。

BIN := bin/mcp-conductor

.PHONY: build run test vet check fmt web-install web-dev web-build build-backend build-console dev docker-up docker-down db-migrate

## 一键构建（默认：先构建前端，嵌入真实 Console 后产出单二进制）
## 前端经 go:embed 打包进二进制；不跑这一步、直接 go build 会得到占位 Console。
build:
	npm --prefix web run build
	rm -rf internal/console/dist && mkdir -p internal/console/dist && cp -r web/dist/* internal/console/dist/
	mkdir -p bin && go build -o $(BIN) ./cmd/conductor

## 构建并运行单二进制（带真实 Console）
run: build
	./$(BIN)

## 仅构建后端（使用当前 internal/console/dist 内容；迭代后端时用）
build-backend:
	mkdir -p bin && go build -o $(BIN) ./cmd/conductor

## 单元测试
test:
	go test ./...

## 静态检查
vet:
	go vet ./...

## 本地质量门禁（Go 静态检查、单元测试、前端构建与类型检查）
check: vet test web-build

## 格式化（仅改动的包由 IDE 校验，此处供整库使用）
fmt:
	gofmt -w ./cmd ./internal

## 前端依赖安装
web-install:
	npm --prefix web install

## 前端开发服务器（代理 /api 与 /mcp 到 :18110，热更新）
web-dev:
	npm --prefix web run dev

## 仅构建前端产物（go:embed 需要它被拷入 internal/console/dist）
web-build:
	npm --prefix web run build

## 兼容别名：等同 build
build-console: build

## 本地一键启动（等同 run）
dev: run

## 数据库/队列基础设施已由 docker compose up 提供
docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

## 把 database/schema.sql 应用到空 MySQL 库（连宿主机实例，见 docker-compose）
## 需先提供 DSN：CONDUCTOR_DATABASE_DSN='user:pass@tcp(host:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4' make db-migrate
db-migrate:
	@test -n "$(CONDUCTOR_DATABASE_DSN)" || (echo "请先设置 CONDUCTOR_DATABASE_DSN（连宿主机 MySQL 的 DSN）后重试"; exit 1)
	@CONDUCTOR_DATABASE_DSN="$(CONDUCTOR_DATABASE_DSN)" go run ./cmd/migrate
