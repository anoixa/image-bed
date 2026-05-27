package main

import (
	"fmt"
	_ "time/tzdata"

	_ "github.com/anoixa/image-bed/docs"

	"github.com/anoixa/image-bed/cmd"
	"github.com/anoixa/image-bed/config"
)

func main() {
	fmt.Printf("image bed %s (%s)\n", config.Version, config.CommitHash)
	cmd.Execute()
}
