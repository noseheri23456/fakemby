//go:build ignore

// 开发期一次性脚本，用 `go run scripts/dev/query_db.go` 执行。
// 加 ignore 标签是为了让 `go build ./...` / `go vet ./...` 跳过它——
// 同目录下有两个 main() 会让整个模块构建失败。
package main

import (
	"fmt"
	"log"

	"github.com/fakemby/fakemby/internal/database"
)

func main() {
	db, err := database.Init("./fakemby.db", true)
	if err != nil {
		log.Fatalf("db init error: %v", err)
	}

	type MediaSource struct {
		ID  string
		URL string
	}
	var sources []MediaSource
	db.Table("media_sources").Find(&sources)
	for _, s := range sources {
		fmt.Printf("Source %s: %s\n", s.ID, s.URL)
	}
}
