package cmd

import (
	"os"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/utils"
)

var commandLog = utils.ForModule("Command")

func initCommandLogger() error {
	if err := config.InitConfig(); err != nil {
		return err
	}
	utils.InitLogger(config.IsDevelopment())
	return nil
}

func exitWithErrorf(format string, args ...any) {
	commandLog.Errorf(format, args...)
	os.Exit(1)
}
