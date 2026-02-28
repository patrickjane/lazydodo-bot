package config

import (
	"encoding/json"
	"errors"
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
	Password string `json:"password"`
}

type ConfigRcon struct {
	Servers           []ConfigRconServer `json:"servers"`
	QueryEverySeconds int                `json:"queryEverySeconds"`
}

type ConfigDiscord struct {
	ChannelIDStatus     string `json:"channelIDStatus"`
	ChannelIDJoinLeave  string `json:"channelIDJoinLeave"`
	ChannelIDJoinEvents string `json:"channelIDEvents"`
	BotToken            string `json:"botToken"`
	Tag                 string `json:"tag"`
	CachePath           string `json:"cachePath"`
	ShowJoinLeave       bool   `json:"showJoinLeave"`
	PinPlayerList       bool   `json:"pinPlayerList"`

	Eventer struct {
		Enabled            bool            `json:"enabled"`
		ReminderOffsets    []time.Duration `json:""`
		ReminderOffsetsRaw []string        `json:"reminderOffsets"`
	} `json:"eventer"`
}

type Config struct {
	Rcon    ConfigRcon    `json:"rcon"`
	Discord ConfigDiscord `json:"discord"`

	LogFile string `json:"logFile"`
}

var GlobalConfig Config

func ParseConfig() {
	res := _parseConfig()

	if res.Rcon.Servers == nil || len(res.Rcon.Servers) == 0 {
		slog.Info(fmt.Sprintf("No RCON servers configured"))
		os.Exit(1)
	}

	if res.Discord.BotToken == "" {
		slog.Info(fmt.Sprintf("No discord bot token configured"))
		os.Exit(1)
	}

	if res.Discord.ChannelIDStatus == "" {
		slog.Info(fmt.Sprintf("No discord channel ID configured"))
		os.Exit(1)
	}

	if res.Discord.ChannelIDJoinLeave == "" {
		res.Discord.ChannelIDJoinLeave = res.Discord.ChannelIDStatus
	}

	if res.Discord.Tag == "" {
		res.Discord.Tag = "lazydodobot"
	}

	if res.Discord.CachePath == "" {
		res.Discord.CachePath = "cache.txt"
	}

	if res.Rcon.QueryEverySeconds == 0 {
		res.Rcon.QueryEverySeconds = 60
	}

	if res.LogFile == "" {
		res.LogFile = "-"
	}

	if len(res.Discord.Eventer.ReminderOffsets) == 0 {
		if len(res.Discord.Eventer.ReminderOffsetsRaw) > 0 {
			o, err := parseDurations(res.Discord.Eventer.ReminderOffsetsRaw)

			if err != nil {
				slog.Info(fmt.Sprintf("Failed to parse reminder offsets: %s", err))
				os.Exit(1)
			}

			res.Discord.Eventer.ReminderOffsets = o
		} else {
			res.Discord.Eventer.ReminderOffsets = []time.Duration{
				24 * time.Hour,
				2 * time.Hour,
				15 * time.Minute,
			}
		}
	}

	if res.Discord.ChannelIDJoinEvents == "-" && res.Discord.Eventer.Enabled {
		slog.Info(fmt.Sprintf("Missing eventer channel definition"))
		os.Exit(1)
	}

	GlobalConfig = res
}

func _parseConfig() Config {
	var res Config
	res.Rcon.Servers = make([]ConfigRconServer, 0)
	res.LogFile = "-"

	var configFile string
	flag.StringVar(&configFile, "config-file", "", "Path to the JSON configuration file")
	flag.Parse()

	if configFile != "" {
		dat, err := os.ReadFile(configFile)

		if err != nil {
			slog.Info(fmt.Sprintf("Failed to read config file %s: %s", configFile, err))
			os.Exit(1)
		}

		if err = json.Unmarshal(dat, &res); err != nil {
			slog.Info(fmt.Sprintf("Failed to parse config file %s: %s", configFile, err))
			os.Exit(1)
		}

		return res
	}

	readString("LOG_FILE", &res.LogFile, "-")

	readString("DISCORD_CHANNEL_ID_STATUS", &res.Discord.ChannelIDStatus, "")
	readString("DISCORD_CHANNEL_ID_JOINLEAVE", &res.Discord.ChannelIDJoinLeave, res.Discord.ChannelIDStatus)
	readString("DISCORD_CHANNEL_ID_EVENTS", &res.Discord.ChannelIDJoinEvents, "-")

	readString("DISCORD_BOT_TOKEN", &res.Discord.BotToken, "")
	readString("DISCORD_MESSAGE_TAG", &res.Discord.Tag, "lazydodobot")
	readString("DISCORD_CACHE_PATH", &res.Discord.CachePath, "cache.txt")
	readBool("DISCORD_SHOW_JOINLEAVE", &res.Discord.ShowJoinLeave, "true")
	readBool("DISCORD_PIN_PLAYERLIST", &res.Discord.PinPlayerList, "true")

	readInt("RCON_QUERY_EVERY_S", &res.Rcon.QueryEverySeconds, "60")

	readBool("EVENTER_ENABLED", &res.Discord.Eventer.Enabled, "false")

	eventerRemindersList := ""
	readString("EVENTER_RMINDERS", &eventerRemindersList, "")

	for _, e := range strings.Split(eventerRemindersList, ",") {
		if len(strings.Trim(e, " ")) > 0 {
			res.Discord.Eventer.ReminderOffsetsRaw = append(res.Discord.Eventer.ReminderOffsetsRaw, strings.Trim(e, " "))
		}
	}

	var rconServers string

	readString("RCON_SERVERS", &rconServers, "")

	err := parseRconServers(&res, rconServers)

	if err != nil {
		slog.Error(fmt.Sprintf("Failed to parse env variable RCON_SERVERS: %s", err))
		os.Exit(1)
	}

	return res
}

func parseRconServers(cfg *Config, envValue string) error {
	// parse: ADDRESS1,NAME1,PASSWORD1;ADDRESS2,NAME2,PASSWORD2;ADDRESS3,NAME3,PASSWORD3
	// example: RCON_SERVERS="10.0.0.1:27015,Main Server,secret123;10.0.0.2:27015,Backup Server,backup456"

	if strings.TrimSpace(envValue) == "" {
		return errors.New("RCON_SERVERS env variable is empty")
	}

	entries := strings.Split(envValue, ";")

	for idx, entry := range entries {
		entry = strings.TrimSpace(entry)

		if entry == "" {
			continue // skip empty entries
		}

		parts := strings.Split(entry, ",")

		if len(parts) != 3 {
			return fmt.Errorf("invalid server entry #%d: expected 3 comma-separated fields, got %d (%q)", idx+1, len(parts), entry)
		}

		server := ConfigRconServer{
			Address:  strings.TrimSpace(parts[0]),
			Name:     strings.TrimSpace(parts[1]),
			Password: strings.TrimSpace(parts[2]),
		}

		if server.Address == "" || server.Name == "" || server.Password == "" {
			return fmt.Errorf("invalid server entry #%d: fields must not be empty (%q)", idx+1, entry)
		}

		cfg.Rcon.Servers = append(cfg.Rcon.Servers, server)
	}

	return nil
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

func readString(name string, target *string, defaultVal string) {
	value := os.Getenv(name)

	if value == "" {
		if defaultVal != "" {
			value = defaultVal
		} else {
			slog.Error(fmt.Sprintf("Missing env variable %s", name))
			os.Exit(1)
		}
	}

	if target == nil {
		slog.Error(fmt.Sprintf("Target for env variable %s is nil", name))
		os.Exit(1)
	}

	*target = value
}

func readInt(name string, target *int, defaultVal string) {
	var strVal string

	readString(name, &strVal, defaultVal)

	i, err := strconv.Atoi(strVal)

	if err != nil {
		slog.Error(fmt.Sprintf("Value for env variable %s is not a valid number: %s", name, strVal))
		os.Exit(1)
	}

	if target == nil {
		slog.Error(fmt.Sprintf("Target for env variable %s is nil", name))
		os.Exit(1)
	}

	*target = i
}

func readBool(name string, target *bool, defaultVal string) {
	var strVal string

	readString(name, &strVal, defaultVal)

	if target == nil {
		slog.Error(fmt.Sprintf("Target for env variable %s is nil", name))
		os.Exit(1)
	}

	*target = strings.ToLower(strVal) == "true"
}
