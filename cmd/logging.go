package cmd

import (
	"os"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/utils"
)

var commandLog = utils.ForModule("Command")

func initCommandLogger() {
	config.InitConfig()
	utils.InitLogger(config.IsDevelopment())
}

func exitWithErrorf(format string, args ...any) {
	commandLog.Errorf(format, args...)
	os.Exit(1)
}
