// 命令 migrate 按文件名顺序执行 migrations/*.sql，用于把开发/测试库表
// 结构应用到指定 MySQL（替代已随 Compose 移除的容器内 mysql 客户端）。
//
// 迁移文件约定为可重复执行的 DDL（0001/0003 用 IF NOT EXISTS、0002 用
// information_schema 守卫），因此执行器不做版本记录表，重复执行安全。
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
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("CONDUCTOR_DATABASE_DSN"), "MySQL DSN（默认取环境变量 CONDUCTOR_DATABASE_DSN）")
	dir := flag.String("dir", "migrations", "迁移 SQL 文件目录")
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

	files, err := filepath.Glob(filepath.Join(*dir, "*.sql"))
	if err != nil {
		log.Fatalf("扫描迁移目录失败: %v", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		log.Fatalf("目录 %q 中没有 .sql 迁移文件", *dir)
	}

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("读取 %s 失败: %v", f, err)
		}
		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			log.Fatalf("应用 %s 失败: %v", filepath.Base(f), err)
		}
		fmt.Printf("applied %s\n", filepath.Base(f))
	}
	fmt.Println("迁移完成：全部迁移文件已按序执行")
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
