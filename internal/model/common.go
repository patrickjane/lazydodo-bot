package model

type PlayerInfo struct {
	Name  string
	Tribe string
}

type ServerInfo struct {
	Name      string `json:"-"`
	Map       string `json:"-"`
	Reachable bool   `json:"-"`

	Day           int
	Players       []PlayerInfo
	ServerVersion string
	Time          string
}
