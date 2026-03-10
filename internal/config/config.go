package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

var Version string

type ConfigRconServer struct {
	Address  string `json:"address"`
	Name     string `json:"name"`
	Map      string `json:"map"`
	Password string `json:"password"`
}

type ConfigRcon struct {
	Servers           []ConfigRconServer `json:"servers"`
	QueryEverySeconds int                `json:"queryEverySeconds"`
}

type ConfigRoot struct {
	LogFile   string `json:"logFile"`
	CachePath string `json:"cachePath"`

	BotToken string `json:"botToken"`

	ServerStatus *struct {
		Rcon ConfigRcon `json:"rcon"`

		DbConnection       string `json:"DbConnection"`
		ChannelID          string `json:"channelID"`
		ChannelIDJoinLeave string `json:"channelIDJoinLeave"`
		ShowJoinLeave      bool   `json:"showJoinLeave"`
	} `json:"serverStatus,ommitempty"`

	Eventer *struct {
		ChannelID          string          `json:"channelID"`
		ReminderOffsets    []time.Duration `json:"-"`
		ReminderOffsetsRaw []string        `json:"reminderOffsets"`
	} `json:"eventer,ommitempty"`

	Crosschat *struct {
		ChannelID             string `json:"channelID"`
		DbConnection          string `json:"DbConnection"`
		WebhookCrosschat      string `json:"WebhookCrosschat"`
		WebhookIdCrosschat    string `json:"-"`
		WebhookTokenCrosschat string `json:"-"`
	} `json:"crosschat,ommitempty"`
}

var Config ConfigRoot

func ParseConfig() {
	var configFile string
	flag.StringVar(&configFile, "config-file", "", "Path to the JSON configuration file")
	flag.Parse()

	if configFile == "" {
		configFile = "config.json"
	}

	dat, err := os.ReadFile(configFile)

	if err != nil {
		slog.Info(fmt.Sprintf("Failed to read config file %s: %s", configFile, err))
		os.Exit(1)
	}

	if err = json.Unmarshal(dat, &Config); err != nil {
		slog.Info(fmt.Sprintf("Failed to parse config file %s: %s", configFile, err))
		os.Exit(1)
	}

	// -------------
	// cache
	// -------------

	if Config.CachePath == "" {
		Config.CachePath = "cache.json"
	}

	// -------------
	// Discord
	// -------------

	if Config.BotToken == "" {
		slog.Info(fmt.Sprintf("No discord bot token configured"))
		os.Exit(1)
	}

	if Config.ServerStatus != nil {
		if Config.ServerStatus.Rcon.Servers == nil || len(Config.ServerStatus.Rcon.Servers) == 0 {
			slog.Info(fmt.Sprintf("No RCON servers configured"))
			os.Exit(1)
		}

		if Config.ServerStatus.Rcon.QueryEverySeconds == 0 {
			Config.ServerStatus.Rcon.QueryEverySeconds = 60
		}

		if Config.ServerStatus.ChannelID == "" {
			slog.Info(fmt.Sprintf("No discord channel ID configured for server status"))
			os.Exit(1)
		}

		if Config.ServerStatus.DbConnection == "" {
			slog.Info(fmt.Sprintf("No db connection configured for server status"))
			os.Exit(1)
		}

		if Config.ServerStatus.ChannelIDJoinLeave == "" {
			Config.ServerStatus.ChannelIDJoinLeave = Config.ServerStatus.ChannelID
		}

	}

	if Config.Eventer != nil {
		if Config.Eventer.ChannelID == "" {
			slog.Info(fmt.Sprintf("No discord channel ID configured for eventer"))
			os.Exit(1)
		}

		if len(Config.Eventer.ReminderOffsets) == 0 {
			if len(Config.Eventer.ReminderOffsetsRaw) > 0 {
				o, err := parseDurations(Config.Eventer.ReminderOffsetsRaw)

				if err != nil {
					slog.Info(fmt.Sprintf("Failed to parse reminder offsets: %s", err))
					os.Exit(1)
				}

				Config.Eventer.ReminderOffsets = o
			} else {
				Config.Eventer.ReminderOffsets = []time.Duration{
					24 * time.Hour,
					2 * time.Hour,
					15 * time.Minute,
				}
			}
		}
	}

	if Config.Crosschat != nil {
		if Config.Crosschat.ChannelID == "" {
			slog.Info(fmt.Sprintf("No discord channel ID configured for crosschat"))
			os.Exit(1)
		}

		if Config.Crosschat.DbConnection == "" {
			slog.Info(fmt.Sprintf("No db connection configured for crosschat"))
			os.Exit(1)
		}

		if Config.Crosschat.WebhookCrosschat != "" {
			id, token := parseWebhookURL(Config.Crosschat.WebhookCrosschat)

			Config.Crosschat.WebhookIdCrosschat = id
			Config.Crosschat.WebhookTokenCrosschat = token
		}

		if len(Config.Crosschat.WebhookIdCrosschat) == 0 || len(Config.Crosschat.WebhookTokenCrosschat) == 0 {
			slog.Info(fmt.Sprintf("Malformed webhook URL"))
			os.Exit(1)
		}
	}
}

func parseDurationString(s string) (time.Duration, error) {
	parts := strings.Fields(strings.TrimSpace(s))
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid duration format: %q", s)
	}

	// Parse number
	value, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number in duration %q: %w", s, err)
	}

	unit := strings.ToLower(parts[1])

	switch unit {
	case "minute", "minutes":
		return time.Duration(value) * time.Minute, nil
	case "hour", "hours":
		return time.Duration(value) * time.Hour, nil
	case "day", "days":
		return time.Duration(value) * 24 * time.Hour, nil
	case "week", "weeks":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid unit in duration %q", s)
	}
}

func parseDurations(durations []string) ([]time.Duration, error) {
	var res []time.Duration

	for _, s := range durations {
		d, err := parseDurationString(s)

		if err != nil {
			return res, err
		}

		res = append(res, d)
	}

	return res, nil
}

func parseWebhookURL(url string) (string, string) {
	parts := strings.Split(url, "/")
	return parts[len(parts)-2], parts[len(parts)-1]
}

func CleanDbString(dsn string) string {
	colon := strings.Index(dsn, ":")
	at := strings.Index(dsn, "@")

	if colon == -1 || at == -1 || colon > at {
		return dsn // not in expected format
	}

	return dsn[:colon+1] + dsn[at:]
}
