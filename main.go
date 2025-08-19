package main

import (
	"log"

	"github.com/anoixa/image-bed/cmd"
	"github.com/anoixa/image-bed/utils/version"
)

func main() {
	log.Printf("image bed %s (%s)", version.Version, version.CommitHash)
	cmd.Execute()
}
