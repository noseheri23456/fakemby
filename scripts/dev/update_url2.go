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

	var count int64
	db.Table("media_sources").Count(&count)
	fmt.Printf("Total media sources: %d\n", count)

	result := db.Exec("UPDATE media_sources SET url = 'https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_1080p_h264.mov'")
	if result.Error != nil {
		log.Fatalf("update error: %v", result.Error)
	}
	
	fmt.Printf("Updated %d media sources to the correct test video URL.\n", result.RowsAffected)

	// Verify
	type MediaSource struct {
		ID  string
		URL string
	}
	var sources []MediaSource
	db.Table("media_sources").Limit(5).Find(&sources)
	for _, s := range sources {
		fmt.Printf("Source %s: %s\n", s.ID, s.URL)
	}
}
