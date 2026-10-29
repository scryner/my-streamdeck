package widgets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	CaffeinateSessionDirEnv        = "MY_STREAMDECK_CAFFEINATE_SESSION_DIR"
	CaffeinateRestoreGenerationEnv = "MY_STREAMDECK_CAFFEINATE_RESTORE_GENERATION"
	CaffeinateSaveGenerationEnv    = "MY_STREAMDECK_CAFFEINATE_SAVE_GENERATION"
)

// Only intent and accumulated active time survive worker replacement. A new
// worker consumes the checkpoint before restoring it and always queries its own
// session before starting any process.
type CaffeinateIntent struct {
	RequestedEnabled bool
	ActiveDuration   time.Duration
	Generation       string
}

type CaffeinateIntentStore interface {
	Load() (CaffeinateIntent, error)
	Save(CaffeinateIntent) error
}

type fileCaffeinateIntentStore struct {
	path              string
	restoreGeneration string
	saveGeneration    string
}

func NewCaffeinateSessionStore(directory, key string) CaffeinateIntentStore {
	return NewCaffeinateSessionStoreForWorker(directory, key, "", "")
}

func NewCaffeinateSessionStoreForWorker(directory, key, restoreGeneration, saveGeneration string) CaffeinateIntentStore {
	if directory == "" {
		return nil
	}
	return fileCaffeinateIntentStore{
		path:              filepath.Join(directory, "caffeinate-"+key+".json"),
		restoreGeneration: restoreGeneration,
		saveGeneration:    saveGeneration,
	}
}

func (s fileCaffeinateIntentStore) Load() (CaffeinateIntent, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return CaffeinateIntent{}, nil
	}
	if err != nil {
		return CaffeinateIntent{}, err
	}
	// A checkpoint belongs to one worker handoff. Consume it before returning
	// any saved ON intent, so a later worker cannot restore the same stale ON
	// after this worker turned OFF or exited without writing a fresh checkpoint.
	// Invalid JSON is consumed too, allowing the next start to recover as OFF.
	if err := os.Remove(s.path); err != nil {
		return CaffeinateIntent{}, fmt.Errorf("consume caffeinate checkpoint: %w", err)
	}
	var intent CaffeinateIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return CaffeinateIntent{}, fmt.Errorf("decode caffeinate checkpoint: %w", err)
	}
	// A checkpoint left behind by a failed consume/write in an earlier worker
	// must not become valid again when filesystem access later recovers.
	if intent.Generation != s.restoreGeneration {
		return CaffeinateIntent{}, nil
	}
	return intent, nil
}

func (s fileCaffeinateIntentStore) Save(intent CaffeinateIntent) error {
	intent.Generation = s.saveGeneration
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
