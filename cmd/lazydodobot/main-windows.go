package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/patrickjane/lazydodo-bot/internal/cache"
	cfg "github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/discord"
	"github.com/patrickjane/lazydodo-bot/internal/utils"
	"golang.org/x/sys/windows/svc"
)

var version = ""

func main() {
	// On Windows: check if running as service
	isService, err := svc.IsWindowsService()
	if err == nil && isService {
		runAsService()
		return
	}

	// Otherwise run normally
	runApp()
}

func runAsService() {
	svc.Run("PlayerListBot", &service{})
}

type service struct{}

func (m *service) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {

	const accepts = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	go runApp()

	status <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				return false, 0
			default:
				// ignore other commands
			}
		}
	}
}

func runApp() {
	var logFile *os.File

	cfg.ParseConfig()

	if cfg.Config.LogFile != "" {
		logFile, err := os.OpenFile(cfg.Config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}

		log.SetOutput(logFile)
	}

	slog.Info(fmt.Sprintf("LazyDodoBot %s", version))
	slog.Info("https://github.com/patrickjane/lazydodo-bot")

	slog.Info(fmt.Sprintf("Initializing cache at %s", cfg.Config.CachePath))

	cache.Init()

	if cfg.Config.Eventer != nil {
		slog.Info("Event monitoring enabled, setting reminders for every event at:")

		for _, r := range cfg.Config.Eventer.ReminderOffsets {
			slog.Info(fmt.Sprintf("   - %s before", utils.FormatDuration(r, utils.English)))
		}
	} else {
		slog.Info("Event monitoring disabled")
	}

	slog.Info("Monitoring the following servers via RCON:")

	if cfg.Config.ServerStatus != nil {
		for _, s := range cfg.Config.ServerStatus.Rcon.Servers {
			slog.Info(fmt.Sprintf("   %s at %s", s.Name, s.Address))
		}

		slog.Info(fmt.Sprintf("Query RCON servers every %d seconds", cfg.Config.ServerStatus.Rcon.QueryEverySeconds))
	}

	if cfg.Config.Crosschat != nil {
		slog.Info("Cross chat enabled")
	}

	slog.Info("Starting discord bot")

	discordBot := discord.NewBot()

	err := discordBot.Start()

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to start discord bot: %s", err))
		os.Exit(1)
	}

	sigShutdown := make(chan os.Signal, 1)
	signal.Notify(sigShutdown, syscall.SIGTERM, syscall.SIGINT)

	<-sigShutdown

	slog.Info("Shutting down.")

	discordBot.Stop()

	if logFile != nil {
		logFile.Close()
	}
}
