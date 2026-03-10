package serverstatus

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/patrickjane/lazydodo-bot/internal/cache"
	cfg "github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/model"
)

const tableServers = "crosschat_servers"

type ServerStatus struct {
	Session *discordgo.Session
	UserID  string

	db           *sql.DB
	queryServers string
}

func NewServerStatus(s *discordgo.Session, userID string) *ServerStatus {
	db, err := sql.Open("mysql", cfg.Config.ServerStatus.DbConnection)

	if err != nil {
		panic(err)
	}

	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxIdleTime(1 * time.Minute)

	return &ServerStatus{
		Session:      s,
		UserID:       userID,
		db:           db,
		queryServers: fmt.Sprintf("SELECT ServerName, ServerStatus FROM %s", tableServers),
	}
}

func (s *ServerStatus) RunServerStatus(fromRcon <-chan map[string]*model.ServerInfo) error {
	var existingMessageId string

	cacheData, err := cache.Get()

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to load server status message id from cache: %s", err))
		return err
	}

	if len(cacheData.DiscordMessageIdStatus) > 0 {
		existingMessageId = cacheData.DiscordMessageIdStatus
	}

	for {
		select {
		case ifos := <-fromRcon:
			err := s.fetchPlayerInfosFromDb(ifos)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to retrieve server info from db: %s", err))
			}

			msgId, err := s.updatePlayerList(existingMessageId, ifos)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to send player list update to discord: %s", err))
			}

			existingMessageId = msgId

			err = cache.Update(func(k *cache.CacheData) {
				k.DiscordMessageIdStatus = existingMessageId
			})

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to store server status message id in cache: %s", err))
			}
		}
	}
}

func (s *ServerStatus) sendNotifyMessage(server string, player string, joined bool) error {
	var err error

	if joined {
		_, err = s.Session.ChannelMessageSend(cfg.Config.ServerStatus.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s joined the server", server, player))
	} else {
		_, err = s.Session.ChannelMessageSend(cfg.Config.ServerStatus.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s left the server", server, player))
	}

	return err
}

func (s *ServerStatus) sendMoveMessage(player string, oldserver string, newserver string) error {
	var err error
	_, err = s.Session.ChannelMessageSend(cfg.Config.ServerStatus.ChannelIDJoinLeave, fmt.Sprintf("[%s -> %s] %s moved servers", oldserver, newserver, player))
	return err
}

func (s *ServerStatus) updatePlayerList(existingMessageId string, serverStatusMap map[string]*model.ServerInfo) (string, error) {
	// assemble message payload from server infos

	payload := &discordgo.MessageSend{
		Content: fmt.Sprintf("# Server status"),
	}

	keys := make([]string, 0, len(serverStatusMap))

	for k := range serverStatusMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, serverName := range keys {
		serverInfo := serverStatusMap[serverName]

		body := "No players online"
		color := 0x57F287 // Discord green

		if len(serverInfo.Players) > 0 {
			players := []string{}

			for _, player := range serverInfo.Players {
				players = append(players, fmt.Sprintf("- %s (%s)", player.Name, player.Tribe))
			}

			body = "Players:\n" + strings.Join(players, "\n")
		}

		if !serverInfo.Reachable {
			color = 0xc1121f
			body = "Server unreachable"
		}

		payload.Embeds = append(payload.Embeds, &discordgo.MessageEmbed{
			Title:       serverName,
			Description: body,
			Color:       color,
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Day: %d, time: %s", serverInfo.Day, serverInfo.Time),
			},
		})
	}

	// check if we already have the (pinned) message, then we edit it instead of send a new message

	theMessage, err := s.fetchExistingMessage(existingMessageId)

	if err != nil {
		return "", fmt.Errorf("fetchExistingMessage: %s", err)
	}

	// actually send the updat to discord (edit or new)

	if theMessage != nil {
		edit := &discordgo.MessageEdit{
			ID:      theMessage.ID,
			Channel: cfg.Config.ServerStatus.ChannelID,
			Content: &payload.Content, // replace content
			Embeds:  &payload.Embeds,  // replace embeds array
		}

		theMessage, err = s.Session.ChannelMessageEditComplex(edit)

		if err != nil {
			return "", fmt.Errorf("ChannelMessageEditComplex: %s", err)
		}
	} else {
		theMessage, err = s.Session.ChannelMessageSendComplex(cfg.Config.ServerStatus.ChannelID, payload)

		if err != nil {
			return "", fmt.Errorf("ChannelMessageSendComplex: %s", err)
		}
	}

	// return message id for faster lookup next time

	return theMessage.ID, nil
}

func (s *ServerStatus) fetchExistingMessage(existingMessageId string) (*discordgo.Message, error) {
	if len(existingMessageId) > 0 {
		return s.Session.ChannelMessage(cfg.Config.ServerStatus.ChannelID, existingMessageId)
	}

	msgs, err := s.Session.ChannelMessages(cfg.Config.ServerStatus.ChannelID, 100, "", "", "")

	if err != nil {
		return nil, err
	}

	for _, m := range msgs {
		if m.Author != nil && m.Author.ID == s.UserID && strings.Contains(m.Content, "Online players") {
			return m, nil
		}
	}

	return nil, nil
}

func (s *ServerStatus) fetchPlayerInfosFromDb(serverInfos map[string]*model.ServerInfo) error {
	rows, err := s.db.Query(s.queryServers)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to query database for chat messages: %s", err))
		return err
	}

	defer rows.Close()

	// Loop through rows, using Scan to assign column data to struct fields.

	for rows.Next() {
		serverName := ""
		serverStatus := ""

		if err := rows.Scan(&serverName, &serverStatus); err != nil {
			slog.Error(fmt.Sprintf("Failed to deserialize row for server status: %s", err))
			continue
		}

		found := false

		for _, ifo := range serverInfos {
			if ifo.Map != serverName {
				continue
			}

			found = true

			err := json.Unmarshal([]byte(serverStatus), ifo)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to deserialize server status json for server %s: %s", serverName, err))
			}
		}

		if !found {
			slog.Warn(fmt.Sprintf("Ignoring server info for unknown/unexpected server: %s", serverName))
		}
	}

	if err := rows.Err(); err != nil {
		slog.Error(fmt.Sprintf("Query database for chat messages returned error: %s", err))
		return err
	}

	return nil
}
