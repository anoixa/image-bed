package main

import (
	"fmt"
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

	fmt.Printf("image bed %s (%s)\n", config.Version, config.CommitHash)
	cmd.Execute()
}
