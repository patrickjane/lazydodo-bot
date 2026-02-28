package discord

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/model"
)

type DiscordBot struct {
	cfg     config.ConfigDiscord
	userID  string
	session *discordgo.Session
}

func NewBot(cfg config.ConfigDiscord) *DiscordBot {
	return &DiscordBot{
		cfg: cfg,
	}
}

func (bot *DiscordBot) Start(updateChan <-chan map[string]*model.ServerInfo) error {
	var existingMessageId string

	i, err := readMessageId(bot.cfg.CachePath)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to read cache path %s: %s", bot.cfg.CachePath, err))
		return err
	}

	if len(i) > 0 {
		existingMessageId = i
	}

	s, err := discordgo.New("Bot " + bot.cfg.BotToken)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to create new discord bot/connection: %v", err))
		return err
	}

	bot.session = s

	// Opening a Gateway session is optional for pure REST, but it populates s.State.User.

	if err := s.Open(); err != nil {
		slog.Error(fmt.Sprintf("Failed to open discord session: %v", err))
		return err
	}

	// Prefer state if we have an active gateway session

	if s.State != nil && s.State.User != nil && s.State.User.ID != "" {
		bot.userID = s.State.User.ID
	} else {
		u, err := s.User("@me")
		if err == nil {
			bot.userID = u.ID
		}
	}

	var lastInfos map[string]*model.ServerInfo
	lastInfos = nil

	for ifos := range updateChan {
		msgId, err := bot.updatePlayerList(existingMessageId, ifos)

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to send player list update to discord: %s", err))
		}

		existingMessageId = msgId

		err = writeMessageId(bot.cfg.CachePath, existingMessageId)

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to write cache path %s: %s", bot.cfg.CachePath, err))
		}

		if bot.cfg.ShowJoinLeave {
			prevPlayerServer := make(map[string]string)
			currPlayerServer := make(map[string]string)

			// Build previous player -> server map
			if lastInfos != nil {
				for server, info := range lastInfos {
					for _, player := range info.Players {
						prevPlayerServer[player] = server
					}
				}
			}

			// Build current player -> server map
			for server, info := range ifos {
				for _, player := range info.Players {
					currPlayerServer[player] = server
				}
			}

			// Detect joins + moves
			for player, newServer := range currPlayerServer {
				oldServer, existedBefore := prevPlayerServer[player]

				switch {
				case !existedBefore:
					// Pure join
					if err := bot.sendNotifyMessage(newServer, player, true); err != nil {
						slog.Error(fmt.Sprintf("Failed to send join message: %s", err))
					}

				case oldServer != newServer:
					// Move detected
					if err := bot.sendMoveMessage(player, oldServer, newServer); err != nil {
						slog.Error(fmt.Sprintf("Failed to send move message: %s", err))
					}
				}
			}

			// Detect leaves
			for player, oldServer := range prevPlayerServer {
				if _, stillOnline := currPlayerServer[player]; !stillOnline {
					if err := bot.sendNotifyMessage(oldServer, player, false); err != nil {
						slog.Error(fmt.Sprintf("Failed to send leave message: %s", err))
					}
				}
			}
		}

		lastInfos = make(map[string]*model.ServerInfo)

		for server, info := range ifos {
			playersCopy := make([]string, len(info.Players))
			copy(playersCopy, info.Players)

			lastInfos[server] = &model.ServerInfo{
				Name:      info.Name,
				Players:   playersCopy,
				Reachable: info.Reachable,
			}
		}
	}

	return nil
}

func (bot *DiscordBot) Stop() {
	if bot.session != nil {
		bot.session.Close()
	}
}

func (bot *DiscordBot) sendNotifyMessage(server string, player string, joined bool) error {
	var err error

	if joined {
		_, err = bot.session.ChannelMessageSend(bot.cfg.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s joined the server", server, player))
	} else {
		_, err = bot.session.ChannelMessageSend(bot.cfg.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s left the server", server, player))
	}

	return err
}

func (bot *DiscordBot) sendMoveMessage(player string, oldserver string, newserver string) error {
	var err error
	_, err = bot.session.ChannelMessageSend(bot.cfg.ChannelIDJoinLeave, fmt.Sprintf("[%s -> %s] %s moved servers", oldserver, newserver, player))
	return err
}

func (bot *DiscordBot) updatePlayerList(existingMessageId string, serverStatusMap map[string]*model.ServerInfo) (string, error) {
	// assemble message payload from server infos

	payload := &discordgo.MessageSend{
		Content: fmt.Sprintf("# Online players\nFrom: <%s>", bot.cfg.Tag),
	}

	keys := make([]string, 0, len(serverStatusMap))

	for k := range serverStatusMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, serverName := range keys {
		serverInfo := serverStatusMap[serverName]

		playerlist := "No players online"
		color := 0x343434

		if len(serverInfo.Players) > 0 {
			color = 0x57F287 // Discord green
			players := []string{}

			for _, player := range serverInfo.Players {
				players = append(players, fmt.Sprintf("- %s", player))
			}

			playerlist = strings.Join(players, "\n")
		}

		if !serverInfo.Reachable {
			color = 0xc1121f
			playerlist = "Server unreachable"
		}

		payload.Embeds = append(payload.Embeds, &discordgo.MessageEmbed{
			Title:       serverName,
			Description: playerlist,
			Color:       color,
		})
	}

	// check if we already have the (pinned) message, then we edit it instead of send a new message

	theMessage, err := bot.fetchExistingMessage(existingMessageId)

	if err != nil {
		return "", fmt.Errorf("fetchExistingMessage: %s", err)
	}

	// actually send the updat to discord (edit or new)

	if theMessage != nil {
		edit := &discordgo.MessageEdit{
			ID:      theMessage.ID,
			Channel: bot.cfg.ChannelIDStatus,
			Content: &payload.Content, // replace content
			Embeds:  &payload.Embeds,  // replace embeds array
		}

		theMessage, err = bot.session.ChannelMessageEditComplex(edit)

		if err != nil {
			return "", fmt.Errorf("ChannelMessageEditComplex: %s", err)
		}
	} else {
		theMessage, err = bot.session.ChannelMessageSendComplex(bot.cfg.ChannelIDStatus, payload)

		if err != nil {
			return "", fmt.Errorf("ChannelMessageSendComplex: %s", err)
		}
	}

	if bot.cfg.PinPlayerList {
		// Pin target message

		if err := bot.session.ChannelMessagePin(bot.cfg.ChannelIDStatus, theMessage.ID); err != nil {
			return "", fmt.Errorf("ChannelMessagePin: %s", err)
		}
	}

	// return message id for faster lookup next time

	return theMessage.ID, nil
}

func (bot *DiscordBot) fetchExistingMessage(existingMessageId string) (*discordgo.Message, error) {
	if len(existingMessageId) > 0 {
		return bot.session.ChannelMessage(bot.cfg.ChannelIDStatus, existingMessageId)
	}

	msgs, err := bot.session.ChannelMessages(bot.cfg.ChannelIDStatus, 100, "", "", "")

	if err != nil {
		return nil, err
	}

	for _, m := range msgs {
		if m.Author != nil && m.Author.ID == bot.userID && strings.Contains(m.Content, bot.cfg.Tag) {
			return m, nil
		}
	}

	return nil, nil
}

func writeMessageId(path string, data string) error {
	// 0600 = user read/write, no permissions for others
	return os.WriteFile(path, []byte(data), 0600)
}

func readMessageId(path string) (string, error) {
	b, err := os.ReadFile(path)

	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File does not exist → return empty string and no error
			return "", nil
		}

		return "", err
	}

	return string(b), nil
}
