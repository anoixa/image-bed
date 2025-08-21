package main

import (
	"github.com/anoixa/image-bed/utils"
	"log"

	"github.com/anoixa/image-bed/cmd"
)

func main() {
	log.Printf("image bed %s (%s)", utils.Version, utils.CommitHash)
	cmd.Execute()
}
