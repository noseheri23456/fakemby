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
