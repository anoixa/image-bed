package main

import (
	"log"
	"time"

	_ "github.com/anoixa/image-bed/docs"

	"github.com/anoixa/image-bed/cmd"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/utils/pool"
)

func init() {
	var cstZone = time.FixedZone("CST", 8*3600) // 东八
	time.Local = cstZone
}

func main() {
	pool.InitProcessEnv()

	log.Printf("image bed %s (%s)", config.Version, config.CommitHash)
	cmd.Execute()
}
