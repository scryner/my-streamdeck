package widgets

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const CaffeinateSessionDirEnv = "MY_STREAMDECK_CAFFEINATE_SESSION_DIR"

// Only intent and accumulated active time survive worker replacement. A new
// worker always queries its own session before starting any process.
type CaffeinateIntent struct {
	RequestedEnabled bool
	ActiveDuration   time.Duration
}

type CaffeinateIntentStore interface {
	Load() (CaffeinateIntent, error)
	Save(CaffeinateIntent) error
}

type fileCaffeinateIntentStore struct{ path string }

func NewCaffeinateSessionStore(directory, key string) CaffeinateIntentStore {
	if directory == "" {
		return nil
	}
	return fileCaffeinateIntentStore{path: filepath.Join(directory, "caffeinate-"+key+".json")}
}

func (s fileCaffeinateIntentStore) Load() (CaffeinateIntent, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return CaffeinateIntent{}, nil
	}
	if err != nil {
		return CaffeinateIntent{}, err
	}
	var intent CaffeinateIntent
	err = json.Unmarshal(data, &intent)
	return intent, err
}

func (s fileCaffeinateIntentStore) Save(intent CaffeinateIntent) error {
	data, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".caffeinate-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.path)
}
