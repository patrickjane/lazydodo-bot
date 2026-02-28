package rcon

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gorcon/rcon"
	"github.com/patrickjane/lazydodo-bot/internal/config"
	"github.com/patrickjane/lazydodo-bot/internal/model"
)

func Run(cfg config.ConfigRcon, updateChan chan<- map[string]*model.ServerInfo, errorChan chan<- error) error {
	ticker := time.NewTicker(time.Duration(cfg.QueryEverySeconds) * time.Second)
	defer ticker.Stop()

	ifos := make(map[string]*model.ServerInfo)

	for _, rconServerConf := range cfg.Servers {
		ifos[rconServerConf.Name] = &model.ServerInfo{
			Name:      rconServerConf.Name,
			Reachable: true,
			Players:   make([]string, 0),
		}
	}

	for range ticker.C {
		for _, rconServerConfig := range cfg.Servers {
			players, err := queryServer(rconServerConfig)

			if err != nil {
				slog.Error(fmt.Sprintf("Failed to query server %s: %s", rconServerConfig.Address, err))

				ifos[rconServerConfig.Name].Reachable = false
				ifos[rconServerConfig.Name].Players = []string{}
			} else {
				ifos[rconServerConfig.Name].Reachable = true
				ifos[rconServerConfig.Name].Players = players
			}
		}

		updateChan <- ifos
	}

	return nil
}

func queryServer(cfg config.ConfigRconServer) ([]string, error) {
	conn, err := rcon.Dial(cfg.Address, cfg.Password)

	slog.Debug(fmt.Sprintf("Opening RCON connection to %s (%s) ...", cfg.Address, cfg.Name))

	if err != nil {
		return nil, err
	}

	response, err := conn.Execute("ListPlayers")

	if err != nil {
		return nil, err
	}

	var newPlayers []string

	for _, raw := range strings.Split(response, "\n") {
		rawTrimmed := strings.Trim(raw, " ")

		if !strings.Contains(rawTrimmed, "No Players Connected") {
			name, err := parseName(rawTrimmed)

			if err != nil {
				return nil, err
			}

			if len(name) > 0 {
				newPlayers = append(newPlayers, name)
			}
		}
	}

	conn.Close()

	return newPlayers, nil
}

func parseName(line string) (string, error) {
	if len(strings.Trim(line, " ")) == 0 {
		return "", nil
	}

	// player list return from RCON command looks like this:
	// '
	// 0. Player 1, 00038213822312333223213123abc2
	// 1. Player 2, 00038223123223123213213123abc5
	// 2. Player 3, 00038436382231232132777123abc8
	// '

	// Split at ". " to remove the leading index

	parts := strings.SplitN(line, ". ", 2)

	if len(parts) != 2 {
		return "", fmt.Errorf("invalid format: missing '. '")
	}

	// From the remaining string, take everything before the comma

	rest := parts[1]
	namePart := strings.SplitN(rest, ",", 2)

	if len(namePart) == 0 {
		return "", fmt.Errorf("invalid format: missing ','")
	}

	return strings.TrimSpace(namePart[0]), nil
}
