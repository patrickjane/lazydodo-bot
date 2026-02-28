package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/discord"
	"github.com/patrickjane/lazydodo-bot/internal/model"
	"github.com/patrickjane/lazydodo-bot/internal/rcon"
	"github.com/patrickjane/lazydodo-bot/internal/utils"
)

var version = ""

func main() {
	var logFile *os.File

	config.ParseConfig()

	if config.GlobalConfig.LogFile != "-" {
		logFile, err := os.OpenFile(config.GlobalConfig.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}

		log.SetOutput(logFile)
	}

	slog.Info(fmt.Sprintf("LazyDodoBot %s", version))
	slog.Info("https://github.com/patrickjane/lazydodo-bot")

	if config.GlobalConfig.Discord.Eventer.Enabled {
		slog.Info("Event monitoring enabled, setting reminders for every event at:")

		for _, r := range config.GlobalConfig.Discord.Eventer.ReminderOffsets {
			slog.Info(fmt.Sprintf("   - %s before", utils.FormatDuration(r, utils.English)))
		}

	} else {
		slog.Info("Event monitoring disabled")
	}

	slog.Info("Monitoring the following servers via RCON:")

	for _, s := range config.GlobalConfig.Rcon.Servers {
		slog.Info(fmt.Sprintf("   %s at %s", s.Name, s.Address))
	}

	slog.Info(fmt.Sprintf("Query RCON servers every %d seconds", config.GlobalConfig.Rcon.QueryEverySeconds))

	errorChan := make(chan error)
	updateChan := make(chan map[string]*model.ServerInfo, 100)

	slog.Info("Connecting to discord")

	discordBot := discord.NewBot(config.GlobalConfig.Discord)

	go func() {
		err := discordBot.Start(updateChan)

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to start discord bot: %s", err))
			os.Exit(1)
		}
	}()

	slog.Info("Creating RCON reader")

	go func() {
		err := rcon.Run(config.GlobalConfig.Rcon, updateChan, errorChan)

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to start RCON connection(s): %s", err))
			os.Exit(1)
		}
	}()

	slog.Info("Successfully started.")

	sigShutdown := make(chan os.Signal, 1)
	signal.Notify(sigShutdown, syscall.SIGTERM, syscall.SIGINT)

	for {
		select {
		case <-sigShutdown:
			slog.Info("Shutting down.")
			discordBot.Stop()

			if logFile != nil {
				logFile.Close()
			}

			return
		case err := <-errorChan:
			slog.Error(fmt.Sprintf("RCON error: %s", err))
		}
	}
}
