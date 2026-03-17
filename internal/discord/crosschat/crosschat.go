package crosschat

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
	_ "github.com/go-sql-driver/mysql"
	"github.com/patrickjane/lazydodo-bot/internal/cache"
	cfg "github.com/patrickjane/lazydodo-bot/internal/config"
)

const tableChat = "cross_chat"
const tableServers = "crosschat_servers"

type CrossChat struct {
	db                *sql.DB
	insertStatement   string
	queryChatMessages string
	queryLastRowId    string
}

type ChatMessage struct {
	Id          uint64
	Map         string
	Sender      string
	Message     string
	TribeName   string
	Mode        int
	isPm        int
	PmRecipient string

	MapPrefix string
}

func NewCrossChat() (*CrossChat, error) {
	db, err := sql.Open("mysql", cfg.Config.Crosschat.DbConnection)

	if err != nil {
		return nil, err
	}

	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxIdleTime(1 * time.Minute)

	// 1: sender
	// 2: message

	insertStatement := fmt.Sprintf("INSERT INTO %s (`EOSid`, `Map`, `Sender`, `Message`, `SenderPlatform`, `TribeName`, `TribeId`, `Mode`, `Tags`, `timestamp`, `isPm`, `PmRecipient`) VALUES ('0', 'Discord', ?, ?, 1, '', 0, 0, '', CURRENT_TIMESTAMP, 0, '');", tableChat)
	queryChatMessages := fmt.Sprintf("SELECT Id, Map, Sender, Message, TribeName, Mode, isPm, PmRecipient FROM %s WHERE Id > ? and mode = 0 and isPm = 0 and Map != 'Discord' order by id asc", tableChat)
	queryLastRowId := fmt.Sprintf("SELECT max(Id) FROM %s", tableChat)

	return &CrossChat{db, insertStatement, queryChatMessages, queryLastRowId}, nil
}

func (s *CrossChat) Run(session *discordgo.Session, fromDiscord <-chan ChatMessage) error {
	cacheData, err := cache.Get()

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to load last chat query time from cache: %s", err))
		return err
	}

	ticker := time.NewTicker(time.Duration(500) * time.Millisecond)
	defer ticker.Stop()

	lastId := cacheData.DbLastRowIdChat

	if lastId == 0 {
		id, err := s.fetchLastRowId()

		if err != nil {
			return err
		}

		slog.Debug(fmt.Sprintf("Fetched last row id from database (%d)", id))

		lastId = id
	} else {
		slog.Debug(fmt.Sprintf("Fetched last row id from cache (%d)", lastId))
	}

	for {
		select {
		case msg := <-fromDiscord:
			slog.Debug(fmt.Sprintf("Got message from discord: %s", msg.Message))

			err := s.insertChatRow(msg.Sender, msg.Message)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to store incoming discord message in database: %s", err))
			}

		case <-ticker.C:
			messages, err := s.fetchChatMessages(lastId)

			if err != nil {
				continue
			}

			for _, m := range messages {
				slog.Debug(fmt.Sprintf("Forward message to discord: %d %s", m.Id, m.Message))

				userNameString := fmt.Sprintf("[%s] %s (%s)", m.MapPrefix, m.Sender, m.TribeName)

				if len(m.TribeName) == 0 {
					userNameString = fmt.Sprintf("[%s] %s", m.MapPrefix, m.Sender)
				}

				_, err := session.WebhookExecute(cfg.Config.Crosschat.WebhookIdCrosschat, cfg.Config.Crosschat.WebhookTokenCrosschat,
					false, &discordgo.WebhookParams{
						Content:  m.Message,
						Username: userNameString,
					})

				if err != nil {
					slog.Error(fmt.Sprintf("Failed to send message to discord: %s", err))
				}

				lastId = m.Id
			}

			err = cache.Update(func(k *cache.CacheData) {
				k.DbLastRowIdChat = lastId
			})

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to store last message query time in cache: %s", err))
			}
		}
	}

	return nil
}

func (s *CrossChat) fetchChatMessages(lastId uint64) ([]ChatMessage, error) {
	rows, err := s.db.Query(s.queryChatMessages, lastId)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to query database for chat messages: %s", err))
		return nil, err
	}

	defer rows.Close()

	var res []ChatMessage

	// Loop through rows, using Scan to assign column data to struct fields.

	for rows.Next() {
		var row ChatMessage

		if err := rows.Scan(&row.Id, &row.Map, &row.Sender, &row.Message, &row.TribeName, &row.Mode, &row.isPm, &row.PmRecipient); err != nil {
			slog.Error(fmt.Sprintf("Failed to deserialize row for chat messages: %s", err))
			continue
		}

		row.MapPrefix = generatePrefixFromMap(row.Map)

		res = append(res, row)
	}

	if err := rows.Err(); err != nil {
		slog.Error(fmt.Sprintf("Query database for chat messages returned error: %s", err))
		return nil, err
	}

	return res, nil
}

func (s *CrossChat) fetchLastRowId() (uint64, error) {
	rows, err := s.db.Query(s.queryLastRowId)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to query database for last row id: %s", err))
		return 0, err
	}

	defer rows.Close()

	var res uint64

	// Loop through rows, using Scan to assign column data to struct fields.

	if rows.Next() {
		if err := rows.Scan(&res); err != nil {
			slog.Error(fmt.Sprintf("Failed to deserialize row for chat messages: %s", err))
			return 0, err
		}
	}

	if err := rows.Err(); err != nil {
		slog.Error(fmt.Sprintf("Query database for last row id returned error: %s", err))
		return 0, err
	}

	return res, nil
}

func (s *CrossChat) insertChatRow(sender string, message string) error {
	_, err := s.db.Exec(s.insertStatement, sender, message)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to insert chat message into database: %s", err))
		return err
	}

	return nil
}

func generatePrefixFromMap(s string) string {
	// Remove suffix
	name := strings.TrimSuffix(s, "_WP")

	if name == "BobsMissions" {
		return "Club Ark"
	}

	// Collect uppercase letters
	var caps []rune
	for _, r := range name {
		if unicode.IsUpper(r) {
			caps = append(caps, r)
		}
	}

	// CamelCase case
	if len(caps) >= 2 {
		return strings.ToUpper(string(caps[:2]))
	}

	// Regular word case
	if len(name) >= 2 {
		return strings.ToUpper(name[:2])
	}

	return strings.ToUpper(name)
}
