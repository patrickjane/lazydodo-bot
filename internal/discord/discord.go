package discord

import (
	"fmt"
	"log/slog"
	"os"

	_ "time/tzdata"

	"github.com/bwmarrin/discordgo"
	cfg "github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/discord/crosschat"
	"github.com/patrickjane/lazydodo-bot/internal/discord/eventer"
	"github.com/patrickjane/lazydodo-bot/internal/discord/serverstatus"
	"github.com/patrickjane/lazydodo-bot/internal/model"
	"github.com/patrickjane/lazydodo-bot/internal/rcon"
)

type DiscordBot struct {
	session                *discordgo.Session
	serverStatus           *serverstatus.ServerStatus
	rconUpdates            chan map[string]*model.ServerInfo
	chatUpdatesFromDiscord chan crosschat.ChatMessage
}

func NewBot() *DiscordBot {
	return &DiscordBot{
		session:                nil,
		serverStatus:           nil,
		rconUpdates:            make(chan map[string]*model.ServerInfo, 100),
		chatUpdatesFromDiscord: make(chan crosschat.ChatMessage, 100),
	}
}

func (bot *DiscordBot) Start() error {
	slog.Info("Connecting to discord")

	var userID string

	s, err := discordgo.New("Bot " + cfg.Config.BotToken)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to create new discord bot/connection: %v", err))
		return err
	}

	bot.session = s

	// register event monitoring callbacks

	if cfg.Config.Eventer != nil {
		s.AddHandler(eventer.CreateRemindersForEvent)
		s.AddHandler(eventer.UpdateRemindersForEvent)
		s.AddHandler(eventer.DeleteRemindersForEvent)

		s.Identify.Intents = discordgo.IntentsGuildScheduledEvents | discordgo.IntentsGuildMessages
	}

	// Opening a Gateway session is optional for pure REST, but it populates s.State.User.

	if err := s.Open(); err != nil {
		slog.Error(fmt.Sprintf("Failed to open discord session: %v", err))
		return err
	}

	// Prefer state if we have an active gateway session

	if s.State != nil && s.State.User != nil && s.State.User.ID != "" {
		userID = s.State.User.ID
	} else {
		u, err := s.User("@me")
		if err == nil {
			userID = u.ID
		}
	}

	// server status scaffold

	if cfg.Config.ServerStatus != nil {
		slog.Info("Starting server status loop")

		bot.serverStatus = serverstatus.NewServerStatus(bot.session, userID)

		go func() {
			err := rcon.Run(cfg.Config.ServerStatus.Rcon, bot.rconUpdates)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to start RCON connection(s): %s", err))
				os.Exit(1)
			}
		}()

		go func() {
			err := bot.serverStatus.RunServerStatus(bot.rconUpdates)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to start server status loop: %s", err))
				os.Exit(1)
			}
		}()
	}

	// eventer scaffold

	if cfg.Config.Eventer != nil {
		slog.Info("Starting eventer loop")

		go eventer.Run(s)
	}

	// crosschat

	if cfg.Config.Crosschat != nil {
		slog.Info("Starting cross chat loop")

		slog.Info(fmt.Sprintf("Connecting to database '%s'", cfg.CleanDbString(cfg.Config.Crosschat.DbConnection)))

		crossChat, err := crosschat.NewCrossChat()

		bot.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
			if m.Author == nil {
				return
			}

			// Ignore bot messages if you want
			if m.Author.Bot {
				return
			}

			// Detect webhook messages
			if m.WebhookID != "" {
				return
			}

			if m.ChannelID != cfg.Config.Crosschat.ChannelID {
				return
			}

			bot.chatUpdatesFromDiscord <- crosschat.ChatMessage{
				Sender:  m.Author.DisplayName(),
				Message: m.Message.Content,
			}
		})

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to start chat syncer: %s", err))
			os.Exit(1)
		}

		go func() {
			err := crossChat.Run(bot.session, bot.chatUpdatesFromDiscord)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to start ChatSyncer: %s", err))
				os.Exit(1)
			}
		}()
	}

	return nil
}

func (bot *DiscordBot) Stop() {
	if bot.session != nil {
		bot.session.Close()
	}
}
