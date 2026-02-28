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

	cfg := config.ParseConfig()

	if cfg.LogFile != "-" {
		logFile, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}

		log.SetOutput(logFile)
	}

	slog.Info(fmt.Sprintf("LazyDodoBot %s", version))
	slog.Info("https://github.com/patrickjane/lazydodo-bot")

	slog.Info("Monitoring the following servers via RCON:")

	for _, s := range cfg.Rcon.Servers {
		slog.Info(fmt.Sprintf("   %s at %s", s.Name, s.Address))
	}

	slog.Info(fmt.Sprintf("Query servers every %d seconds", cfg.Rcon.QueryEverySeconds))

	errorChan := make(chan error)
	updateChan := make(chan map[string]*model.ServerInfo, 100)

	slog.Info("Connecting to discord")

	discordBot := discord.NewBot(cfg.Discord)

	go func() {
		err := discordBot.Start(updateChan)

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to start discord bot: %s", err))
			os.Exit(1)
		}
	}()

	slog.Info("Creating RCON reader")

	go func() {
		err := rcon.Run(cfg.Rcon, updateChan, errorChan)

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
