package main

import (
	"github.com/anoixa/image-bed/config"
	"log"

	"github.com/anoixa/image-bed/cmd"
)

func main() {
	log.Printf("image bed %s (%s)", config.Version, config.CommitHash)
	cmd.Execute()
}
