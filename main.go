package main

import (
	"log"
	"time"

	"github.com/anoixa/image-bed/config"

	"github.com/anoixa/image-bed/cmd"
)

func init() {
	var cstZone = time.FixedZone("CST", 8*3600) // 东八
	time.Local = cstZone
}

func main() {
	log.Printf("image bed %s (%s)", config.Version, config.CommitHash)
	cmd.Execute()
}
