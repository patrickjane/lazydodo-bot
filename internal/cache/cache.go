package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	cfg "github.com/patrickjane/lazydodo-bot/internal/config"
)

type CacheData struct {
	DbLastRowIdChat        uint64    `json:"dbLastRowIdChat"`
	DbLastQueryServers     time.Time `json:"dbLastQueryServers"`
	DiscordMessageIdStatus string    `json:"discordMessageIdStatus"`
}

type Store struct {
	mu   sync.RWMutex
	file string
	data CacheData
}

var singletonStore *Store

func Init() error {
	singletonStore = &Store{file: cfg.Config.CachePath}

	if err := singletonStore.load(); err != nil {
		return err
	}

	return nil
}

func (s *Store) save() error {
	f, err := os.Create(s.file)

	if err != nil {
		return err
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	return enc.Encode(s.data)
}

func (s *Store) load() error {
	f, err := os.Open(s.file)

	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	defer f.Close()

	return json.NewDecoder(f).Decode(&s.data)
}

func Update(fn func(*CacheData)) error {
	if singletonStore == nil {
		return fmt.Errorf("Cache not initialized")
	}

	singletonStore.mu.Lock()
	defer singletonStore.mu.Unlock()

	fn(&singletonStore.data)

	return singletonStore.save()
}

func Get() (CacheData, error) {
	if singletonStore == nil {
		return CacheData{}, fmt.Errorf("Cache not initialized")
	}

	singletonStore.mu.RLock()
	defer singletonStore.mu.RUnlock()

	return singletonStore.data, nil
}
