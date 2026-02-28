package discord

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/model"
	"github.com/patrickjane/lazydodo-bot/internal/utils"
)

type DiscordBot struct {
	userID  string
	session *discordgo.Session
}

type Reminder struct {
	EventID   string
	EventName string
	EventURL  string
	StartTime time.Time // The actual 24h start time
	RemindAt  time.Time // When the bot should post the message
	Now       bool
}

type ReminderStore struct {
	sync.Mutex
	Pending []Reminder
}

var store = &ReminderStore{Pending: []Reminder{}}
var eventerWorkerTick time.Duration = 1 * time.Second
var cetLocation *time.Location

func init() {
	// Initialize the timezone during startup
	var err error

	cetLocation, err = time.LoadLocation("Europe/Berlin") // "Europe/Berlin" is the standard TZ database name for CET/CEST

	if err != nil {
		log.Printf("Warning: Could not load CET location: %s", err)
		panic(err)
	}
}

func NewBot(cfg config.ConfigDiscord) *DiscordBot {
	return &DiscordBot{}
}

func (bot *DiscordBot) Start(updateChan <-chan map[string]*model.ServerInfo) error {
	var existingMessageId string

	if cetLocation == nil {
		panic(fmt.Errorf("CetLoc is nil"))
	}

	i, err := readMessageId(config.GlobalConfig.Discord.CachePath)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to read cache path %s: %s", config.GlobalConfig.Discord.CachePath, err))
		return err
	}

	if len(i) > 0 {
		existingMessageId = i
	}

	s, err := discordgo.New("Bot " + config.GlobalConfig.Discord.BotToken)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to create new discord bot/connection: %v", err))
		return err
	}

	bot.session = s

	// register event monitoring callbacks

	if config.GlobalConfig.Discord.Eventer.Enabled {
		s.AddHandler(createRemindersForEvent)
		s.AddHandler(updateRemindersForEvent)

		s.Identify.Intents = discordgo.IntentsGuildScheduledEvents | discordgo.IntentsGuildMessages
	}

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

	if config.GlobalConfig.Discord.Eventer.Enabled {
		syncExistingEvents(s)
		go reminderWorker(s)
	}

	var lastInfos map[string]*model.ServerInfo
	lastInfos = nil

	for ifos := range updateChan {
		msgId, err := bot.updatePlayerList(existingMessageId, ifos)

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to send player list update to discord: %s", err))
		}

		existingMessageId = msgId

		err = writeMessageId(config.GlobalConfig.Discord.CachePath, existingMessageId)

		if err != nil {
			slog.Error(fmt.Sprintf("Failed to write cache path %s: %s", config.GlobalConfig.Discord.CachePath, err))
		}

		if config.GlobalConfig.Discord.ShowJoinLeave {
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
		_, err = bot.session.ChannelMessageSend(config.GlobalConfig.Discord.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s joined the server", server, player))
	} else {
		_, err = bot.session.ChannelMessageSend(config.GlobalConfig.Discord.ChannelIDJoinLeave, fmt.Sprintf("[%s] %s left the server", server, player))
	}

	return err
}

func (bot *DiscordBot) sendMoveMessage(player string, oldserver string, newserver string) error {
	var err error
	_, err = bot.session.ChannelMessageSend(config.GlobalConfig.Discord.ChannelIDJoinLeave, fmt.Sprintf("[%s -> %s] %s moved servers", oldserver, newserver, player))
	return err
}

func (bot *DiscordBot) updatePlayerList(existingMessageId string, serverStatusMap map[string]*model.ServerInfo) (string, error) {
	// assemble message payload from server infos

	payload := &discordgo.MessageSend{
		Content: fmt.Sprintf("# Online players"),
	}

	keys := make([]string, 0, len(serverStatusMap))

	for k := range serverStatusMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, serverName := range keys {
		serverInfo := serverStatusMap[serverName]

		playerlist := "No players online"
		color := 0x57F287 // Discord green

		if len(serverInfo.Players) > 0 {
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
			Channel: config.GlobalConfig.Discord.ChannelIDStatus,
			Content: &payload.Content, // replace content
			Embeds:  &payload.Embeds,  // replace embeds array
		}

		theMessage, err = bot.session.ChannelMessageEditComplex(edit)

		if err != nil {
			return "", fmt.Errorf("ChannelMessageEditComplex: %s", err)
		}
	} else {
		theMessage, err = bot.session.ChannelMessageSendComplex(config.GlobalConfig.Discord.ChannelIDStatus, payload)

		if err != nil {
			return "", fmt.Errorf("ChannelMessageSendComplex: %s", err)
		}
	}

	if config.GlobalConfig.Discord.PinPlayerList {
		// Pin target message

		if err := bot.session.ChannelMessagePin(config.GlobalConfig.Discord.ChannelIDStatus, theMessage.ID); err != nil {
			return "", fmt.Errorf("ChannelMessagePin: %s", err)
		}
	}

	// return message id for faster lookup next time

	return theMessage.ID, nil
}

func (bot *DiscordBot) fetchExistingMessage(existingMessageId string) (*discordgo.Message, error) {
	if len(existingMessageId) > 0 {
		return bot.session.ChannelMessage(config.GlobalConfig.Discord.ChannelIDStatus, existingMessageId)
	}

	msgs, err := bot.session.ChannelMessages(config.GlobalConfig.Discord.ChannelIDStatus, 100, "", "", "")

	if err != nil {
		return nil, err
	}

	for _, m := range msgs {
		if m.Author != nil && m.Author.ID == bot.userID && strings.Contains(m.Content, "Online players") {
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

/*
Eventer related functions
*/

func createRemindersForEvent(s *discordgo.Session, e *discordgo.GuildScheduledEventCreate) {
	event := e.GuildScheduledEvent
	eventURL := fmt.Sprintf("https://discord.com/events/%s/%s", event.GuildID, event.ID)
	cetTime := event.ScheduledStartTime.In(cetLocation)

	slog.Info(fmt.Sprintf("New event '%s' at %s has been created in discord, scheduling reminders and posting notification",
		event.Name, cetTime.Format("02.01. 15:04")))

	msg := fmt.Sprintf("**Neues Event wurde erstellt** \n\n@everyone\n\n%s", eventURL)

	s.ChannelMessageSend(config.GlobalConfig.Discord.ChannelIDJoinEvents, msg)
	//fmt.Printf("Sending message to discord: %s\n", msg)

	queueReminders(event)
}

func updateRemindersForEvent(s *discordgo.Session, e *discordgo.GuildScheduledEventUpdate) {
	if e.Status != discordgo.GuildScheduledEventStatusScheduled {
		var statusName string

		switch e.Status {
		case discordgo.GuildScheduledEventStatusActive: // 2
			statusName = "Active (Started)"
		case discordgo.GuildScheduledEventStatusCompleted: // 3
			statusName = "Completed"
		case discordgo.GuildScheduledEventStatusCanceled: // 4
			statusName = "Cancelled"
		default:
			statusName = fmt.Sprintf("Unknown (%d)", e.Status)
		}

		slog.Info(fmt.Sprintf("Event '%s' status update: %s", e.Name, statusName))
		return
	}

	slog.Info(fmt.Sprintf("Event '%s' was updated. Rescheduling reminders.", e.Name))

	// 1. Remove any old/stale reminders for this specific event
	removeRemindersForEvent(e.ID)

	// 2. Queue new reminders based on the updated time
	queueReminders(e.GuildScheduledEvent)
}

func removeRemindersForEvent(eventID string) {
	store.Lock()
	defer store.Unlock()

	var updatedList []Reminder
	for _, r := range store.Pending {
		// Only keep reminders that DON'T match the updated EventID
		if r.EventID != eventID {
			updatedList = append(updatedList, r)
		}
	}

	store.Pending = updatedList
}

func queueReminders(event *discordgo.GuildScheduledEvent) {
	store.Lock()
	defer store.Unlock()

	eventURL := fmt.Sprintf("https://discord.com/events/%s/%s", event.GuildID, event.ID)

	for _, offset := range config.GlobalConfig.Discord.Eventer.ReminderOffsets {
		remindTime := event.ScheduledStartTime.Add(-offset)

		if time.Now().Before(remindTime) {
			store.Pending = append(store.Pending, Reminder{
				EventID:   event.ID,
				EventName: event.Name,
				EventURL:  eventURL,
				StartTime: event.ScheduledStartTime, // Store the fixed start time
				RemindAt:  remindTime,
				Now:       false,
			})

			cetTime := remindTime.In(cetLocation)

			slog.Info(fmt.Sprintf("   Scheduling reminder for event '%s' at %s (in %s)", event.Name,
				cetTime.Format("02.01. 15:04"), utils.FormatDuration(remindTime.Sub(time.Now()), utils.English)))
		}
	}

	if time.Now().Before(event.ScheduledStartTime) {
		store.Pending = append(store.Pending, Reminder{
			EventID:   event.ID,
			EventName: event.Name,
			EventURL:  eventURL,
			StartTime: event.ScheduledStartTime, // Store the fixed start time
			RemindAt:  event.ScheduledStartTime,
			Now:       true,
		})

		cetTime := event.ScheduledStartTime.In(cetLocation)

		slog.Info(fmt.Sprintf("   Scheduling reminder for event '%s' at %s (in %s)", event.Name,
			cetTime.Format("02.01. 15:04"), utils.FormatDuration(event.ScheduledStartTime.Sub(time.Now()), utils.English)))
	}
}

func reminderWorker(s *discordgo.Session) {
	ticker := time.NewTicker(time.Duration(eventerWorkerTick))

	for range ticker.C {
		now := time.Now()
		store.Lock()

		var remaining []Reminder

		slog.Debug(fmt.Sprintf("Checking %d reminders:", len(store.Pending)))

		for _, r := range store.Pending {
			cetTime := r.RemindAt.In(cetLocation)

			slog.Debug(fmt.Sprintf("   Event '%s' reminder due at: %s", r.EventName, cetTime.Format("02.01. 15:04")))

			if now.After(r.RemindAt) {
				cetTime := r.StartTime.In(cetLocation)
				timeStr := cetTime.Format("15:04")
				dateStr := cetTime.Format("02.01.")
				msg := ""

				if r.Now {
					msg = fmt.Sprintf("**Reminder** \n\n@everyone\n\nEvent '%s' startet JETZT!\n\n%s",
						r.EventName, r.EventURL)
				} else {
					msg = fmt.Sprintf("**Reminder** \n\n@everyone\n\nEvent '%s' startet am %s um %s! (in %s)\n\n%s",
						r.EventName, dateStr, timeStr, utils.FormatDuration(r.StartTime.Sub(time.Now()).Round(time.Second),
							utils.German), r.EventURL)
				}

				s.ChannelMessageSend(config.GlobalConfig.Discord.ChannelIDJoinEvents, msg)
				//fmt.Printf("Sending message to discord: %s\n", msg)

			} else {
				remaining = append(remaining, r)
			}
		}

		if len(remaining) != len(store.Pending) {
			slog.Info(fmt.Sprintf("Now %d reminders in queue", len(remaining)))
		}

		store.Pending = remaining
		store.Unlock()
	}
}

func syncExistingEvents(s *discordgo.Session) {
	for _, guild := range s.State.Guilds {
		events, err := s.GuildScheduledEvents(guild.ID, false)

		if err != nil {
			continue
		}

		for _, event := range events {
			cetTime := event.ScheduledStartTime.In(cetLocation)

			slog.Info(fmt.Sprintf("Found pending event '%s' at %s", event.Name, cetTime.Format("02.01. 15:04")))

			queueReminders(event)
		}
	}

	slog.Info(fmt.Sprintf("Sync complete. %d reminders in queue", len(store.Pending)))
}
