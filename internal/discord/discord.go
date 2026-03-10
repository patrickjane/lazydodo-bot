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

// func (bot *DiscordBot) runServerStatus(fromRcon <-chan map[string]*model.ServerInfo) error {
// 	var existingMessageId string

// 	cacheData, err := cache.Get()

// 	if err != nil {
// 		slog.Error(fmt.Sprintf("Failed to load server status message id from cache: %s", err))
// 		return err
// 	}

// 	if len(cacheData.DiscordMessageIdStatus) > 0 {
// 		existingMessageId = cacheData.DiscordMessageIdStatus
// 	}

// 	var lastInfos map[string]*model.ServerInfo
// 	lastInfos = nil

// 	for {
// 		select {
// 		case ifos := <-fromRcon:
// 			msgId, err := bot.updatePlayerList(existingMessageId, ifos)

// 			if err != nil {
// 				slog.Error(fmt.Sprintf("Failed to send player list update to discord: %s", err))
// 			}

// 			existingMessageId = msgId

// 			err = cache.Update(func(k *cache.CacheData) {
// 				k.DiscordMessageIdStatus = existingMessageId
// 			})

// 			if err != nil {
// 				slog.Error(fmt.Sprintf("Failed to store server status message id in cache: %s", err))
// 			}

// 			if cfg.Config.ServerStatus.ShowJoinLeave {
// 				prevPlayerServer := make(map[string]string)
// 				currPlayerServer := make(map[string]string)

// 				// Build previous player -> server map
// 				if lastInfos != nil {
// 					for server, info := range lastInfos {
// 						for _, player := range info.Players {
// 							prevPlayerServer[player] = server
// 						}
// 					}
// 				}

// 				// Build current player -> server map
// 				for server, info := range ifos {
// 					for _, player := range info.Players {
// 						currPlayerServer[player] = server
// 					}
// 				}

// 				// Detect joins + moves
// 				for player, newServer := range currPlayerServer {
// 					oldServer, existedBefore := prevPlayerServer[player]

// 					switch {
// 					case !existedBefore:
// 						// Pure join
// 						if err := bot.sendNotifyMessage(newServer, player, true); err != nil {
// 							slog.Error(fmt.Sprintf("Failed to send join message: %s", err))
// 						}

// 					case oldServer != newServer:
// 						// Move detected
// 						if err := bot.sendMoveMessage(player, oldServer, newServer); err != nil {
// 							slog.Error(fmt.Sprintf("Failed to send move message: %s", err))
// 						}
// 					}
// 				}

// 				// Detect leaves
// 				for player, oldServer := range prevPlayerServer {
// 					if _, stillOnline := currPlayerServer[player]; !stillOnline {
// 						if err := bot.sendNotifyMessage(oldServer, player, false); err != nil {
// 							slog.Error(fmt.Sprintf("Failed to send leave message: %s", err))
// 						}
// 					}
// 				}
// 			}

// 			lastInfos = make(map[string]*model.ServerInfo)

// 			for server, info := range ifos {
// 				playersCopy := make([]string, len(info.Players))
// 				copy(playersCopy, info.Players)

// 				lastInfos[server] = &model.ServerInfo{
// 					Name:      info.Name,
// 					Players:   playersCopy,
// 					Reachable: info.Reachable,
// 				}
// 			}
// 		}
// 	}
// }

func (bot *DiscordBot) Stop() {
	if bot.session != nil {
		bot.session.Close()
	}
}

// func (bot *DiscordBot) sendNotifyMessage(server string, player string, joined bool) error {
// 	var err error

// 	if joined {
// 		_, err = bot.session.ChannelMessageSend(cfg.Config.ServerStatus.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s joined the server", server, player))
// 	} else {
// 		_, err = bot.session.ChannelMessageSend(cfg.Config.ServerStatus.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s left the server", server, player))
// 	}

// 	return err
// }

// func (bot *DiscordBot) sendMoveMessage(player string, oldserver string, newserver string) error {
// 	var err error
// 	_, err = bot.session.ChannelMessageSend(cfg.Config.ServerStatus.ChannelIDJoinLeave, fmt.Sprintf("[%s -> %s] %s moved servers", oldserver, newserver, player))
// 	return err
// }

// func (bot *DiscordBot) updatePlayerList(existingMessageId string, serverStatusMap map[string]*model.ServerInfo) (string, error) {
// 	// assemble message payload from server infos

// 	payload := &discordgo.MessageSend{
// 		Content: fmt.Sprintf("# Online players"),
// 	}

// 	keys := make([]string, 0, len(serverStatusMap))

// 	for k := range serverStatusMap {
// 		keys = append(keys, k)
// 	}

// 	sort.Strings(keys)

// 	for _, serverName := range keys {
// 		serverInfo := serverStatusMap[serverName]

// 		playerlist := "No players online"
// 		color := 0x57F287 // Discord green

// 		if len(serverInfo.Players) > 0 {
// 			players := []string{}

// 			for _, player := range serverInfo.Players {
// 				players = append(players, fmt.Sprintf("- %s", player))
// 			}

// 			playerlist = strings.Join(players, "\n")
// 		}

// 		if !serverInfo.Reachable {
// 			color = 0xc1121f
// 			playerlist = "Server unreachable"
// 		}

// 		payload.Embeds = append(payload.Embeds, &discordgo.MessageEmbed{
// 			Title:       serverName,
// 			Description: playerlist,
// 			Color:       color,
// 		})
// 	}

// 	// check if we already have the (pinned) message, then we edit it instead of send a new message

// 	theMessage, err := bot.fetchExistingMessage(existingMessageId)

// 	if err != nil {
// 		return "", fmt.Errorf("fetchExistingMessage: %s", err)
// 	}

// 	// actually send the updat to discord (edit or new)

// 	if theMessage != nil {
// 		edit := &discordgo.MessageEdit{
// 			ID:      theMessage.ID,
// 			Channel: cfg.Config.ServerStatus.ChannelID,
// 			Content: &payload.Content, // replace content
// 			Embeds:  &payload.Embeds,  // replace embeds array
// 		}

// 		theMessage, err = bot.session.ChannelMessageEditComplex(edit)

// 		if err != nil {
// 			return "", fmt.Errorf("ChannelMessageEditComplex: %s", err)
// 		}
// 	} else {
// 		theMessage, err = bot.session.ChannelMessageSendComplex(cfg.Config.ServerStatus.ChannelID, payload)

// 		if err != nil {
// 			return "", fmt.Errorf("ChannelMessageSendComplex: %s", err)
// 		}
// 	}

// 	// return message id for faster lookup next time

// 	return theMessage.ID, nil
// }

// func (bot *DiscordBot) fetchExistingMessage(existingMessageId string) (*discordgo.Message, error) {
// 	if len(existingMessageId) > 0 {
// 		return bot.session.ChannelMessage(cfg.Config.ServerStatus.ChannelID, existingMessageId)
// 	}

// 	msgs, err := bot.session.ChannelMessages(cfg.Config.ServerStatus.ChannelID, 100, "", "", "")

// 	if err != nil {
// 		return nil, err
// 	}

// 	for _, m := range msgs {
// 		if m.Author != nil && m.Author.ID == bot.userID && strings.Contains(m.Content, "Online players") {
// 			return m, nil
// 		}
// 	}

// 	return nil, nil
// }
