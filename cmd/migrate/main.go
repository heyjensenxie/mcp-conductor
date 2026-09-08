// 命令 migrate 执行初始发布的 database/schema.sql，用于把开发/测试库表
// 结构应用到指定 MySQL（替代已随 Compose 移除的容器内 mysql 客户端）。
//
// Schema 使用可重复执行的 DDL（IF NOT EXISTS），因此执行器不做版本记录表，
// 重复执行安全。首个已发布版本后的 Schema 变更应另行引入版本化迁移。
//
// 用法：
//
//	go run ./cmd/migrate -dsn 'user:pass@tcp(host:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4'
//	# 或
//	CONDUCTOR_DATABASE_DSN='...' make db-migrate
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("CONDUCTOR_DATABASE_DSN"), "MySQL DSN（默认取环境变量 CONDUCTOR_DATABASE_DSN）")
	schema := flag.String("schema", "database/schema.sql", "初始化 Schema SQL 文件")
	flag.Parse()

	if strings.TrimSpace(*dsn) == "" {
		log.Fatal("请通过 -dsn 或环境变量 CONDUCTOR_DATABASE_DSN 提供 MySQL DSN（需包含目标库，如 root:xxx@tcp(localhost:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4）")
	}

	db, err := sql.Open("mysql", ensureMultiStatements(*dsn))
	if err != nil {
		log.Fatalf("打开 MySQL 连接失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("连接 MySQL 失败: %v", err)
	}

	content, err := os.ReadFile(*schema)
	if err != nil {
		log.Fatalf("读取 Schema 文件 %s 失败: %v", *schema, err)
	}
	if _, err := db.ExecContext(ctx, string(content)); err != nil {
		log.Fatalf("应用 Schema 文件 %s 失败: %v", *schema, err)
	}
	fmt.Printf("applied %s\n", *schema)
	fmt.Println("初始化完成：Schema 已应用")
}

// ensureMultiStatements 在 DSN 上补充 multiStatements=true，使单个迁移文件
// 可一次性执行多条语句；DSN 原有参数会被保留。
func ensureMultiStatements(dsn string) string {
	if strings.Contains(dsn, "multiStatements") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "multiStatements=true"
}
