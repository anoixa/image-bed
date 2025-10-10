package main

import (
	"log"

	"github.com/anoixa/image-bed/config"

	"github.com/anoixa/image-bed/cmd"
)

func main() {
	log.Printf("image bed %s (%s)", config.Version, config.CommitHash)
	cmd.Execute()
}
