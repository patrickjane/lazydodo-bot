package eventer

import (
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	cfg "github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/utils"
)

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

func Run(s *discordgo.Session) {
	syncExistingEvents(s)

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

				slog.Info(fmt.Sprintf("Sending event '%s' reminder NOW", r.EventName))

				_, err := s.ChannelMessageSend(cfg.Config.Eventer.ChannelID, msg)

				if err != nil {
					slog.Error(fmt.Sprintf("Failed to send discord reminder for event '%s': %s", r.EventName, err))
				}
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

func CreateRemindersForEvent(s *discordgo.Session, e *discordgo.GuildScheduledEventCreate) {
	event := e.GuildScheduledEvent
	eventURL := fmt.Sprintf("https://discord.com/events/%s/%s", event.GuildID, event.ID)
	cetTime := event.ScheduledStartTime.In(cetLocation)

	slog.Info(fmt.Sprintf("New event '%s' at %s has been created in discord, scheduling reminders and posting notification",
		event.Name, cetTime.Format("02.01. 15:04")))

	msg := fmt.Sprintf("**Neues Event wurde erstellt** \n\n@everyone\n\nName: %s\nStart: %s\n%s",
		event.Name, cetTime.Format("02.01. 15:04"), eventURL)

	_, err := s.ChannelMessageSend(cfg.Config.Eventer.ChannelID, msg)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to send discord notification for new event '%s': %s", event.Name, err))
	}

	queueReminders(event)
}

func UpdateRemindersForEvent(s *discordgo.Session, e *discordgo.GuildScheduledEventUpdate) {
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

func DeleteRemindersForEvent(s *discordgo.Session, e *discordgo.GuildScheduledEventDelete) {
	event := e.GuildScheduledEvent
	cetTime := event.ScheduledStartTime.In(cetLocation)

	slog.Info(fmt.Sprintf("Event '%s' at %s has been CANCELLED, posting notification",
		event.Name, cetTime.Format("02.01. 15:04")))

	msg := fmt.Sprintf("**Event wurde GECANCELT** \n\n@everyone\n\nEvent '%s - %s' wurde gecancelt.",
		event.Name, cetTime.Format("02.01. 15:04"))

	_, err := s.ChannelMessageSend(cfg.Config.Eventer.ChannelID, msg)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to send discord notification for cancelled event '%s': %s", event.Name, err))
	}

	removeRemindersForEvent(e.ID)

	store.Lock()
	defer store.Unlock()

	slog.Info(fmt.Sprintf("Now %d reminders in queue", len(store.Pending)))
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

	for _, offset := range cfg.Config.Eventer.ReminderOffsets {
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
