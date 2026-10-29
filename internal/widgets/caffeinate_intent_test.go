package widgets

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCaffeinateCheckpointIsRestoredOnlyOnce(t *testing.T) {
	t.Parallel()
	store := fileCaffeinateIntentStore{path: filepath.Join(t.TempDir(), "intent.json")}
	want := CaffeinateIntent{RequestedEnabled: true, ActiveDuration: 37 * time.Second}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got != want {
		t.Fatalf("first load = %+v, %v; want %+v", got, err, want)
	}
	if _, err := os.Stat(store.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("consumed checkpoint still exists: %v", err)
	}
	// A subsequent worker must start OFF unless its predecessor creates a new
	// checkpoint. This also covers missing files in fresh menu bar sessions.
	got, err = store.Load()
	if err != nil || got != (CaffeinateIntent{}) {
		t.Fatalf("second load = %+v, %v; want default OFF", got, err)
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load()
	if err != nil || got != want {
		t.Fatalf("new checkpoint load = %+v, %v; want %+v", got, err, want)
	}
}

func TestCaffeinateCheckpointCorruptionIsConsumedWithoutPartialRestore(t *testing.T) {
	t.Parallel()
	store := fileCaffeinateIntentStore{path: filepath.Join(t.TempDir(), "intent.json")}
	// The boolean can decode successfully before the duration fails. Never
	// return that partially decoded ON value together with the error.
	data := []byte(`{"RequestedEnabled":true,"ActiveDuration":"invalid"}`)
	if err := os.WriteFile(store.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err == nil || got != (CaffeinateIntent{}) {
		t.Fatalf("corrupt load = %+v, %v; want default OFF and error", got, err)
	}
	got, err = store.Load()
	if err != nil || got != (CaffeinateIntent{}) {
		t.Fatalf("load after corruption = %+v, %v; want default OFF", got, err)
	}
}

func TestCaffeinateCheckpointRemovalFailureCannotRestoreOn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permission enforcement for the current user")
	}
	directory := t.TempDir()
	store := fileCaffeinateIntentStore{path: filepath.Join(directory, "intent.json")}
	if err := store.Save(CaffeinateIntent{RequestedEnabled: true}); err != nil {
		t.Fatal(err)
	}
	// The file remains readable, but deleting it requires directory write
	// permission. Restore permissions before TempDir's cleanup executes.
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0700)
	if _, err := os.ReadFile(store.path); err != nil {
		t.Fatalf("fixture must remain readable: %v", err)
	}
	got, err := store.Load()
	if err == nil || got != (CaffeinateIntent{}) {
		t.Fatalf("load with failed removal = %+v, %v; want default OFF and error", got, err)
	}
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("failed removal unexpectedly removed checkpoint: %v", err)
	}
}

func TestCaffeinateCheckpointRequiresPreviousWorkerGeneration(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	previous := NewCaffeinateSessionStoreForWorker(directory, "key7", "", "worker-1")
	intent := CaffeinateIntent{RequestedEnabled: true, ActiveDuration: 37 * time.Second, Generation: "untrusted"}
	if err := previous.Save(intent); err != nil {
		t.Fatal(err)
	}
	current := NewCaffeinateSessionStoreForWorker(directory, "key7", "worker-1", "worker-2")
	got, err := current.Load()
	if err != nil || !got.RequestedEnabled || got.ActiveDuration != intent.ActiveDuration || got.Generation != "worker-1" {
		t.Fatalf("matching checkpoint = %+v, %v", got, err)
	}
	// Saving a restored snapshot assigns this worker's generation, ignoring
	// the old generation carried by the supplied intent.
	if err := current.Save(got); err != nil {
		t.Fatal(err)
	}
	staleReader := NewCaffeinateSessionStoreForWorker(directory, "key7", "worker-1", "worker-3")
	got, err = staleReader.Load()
	if err != nil || got != (CaffeinateIntent{}) {
		t.Fatalf("mismatched checkpoint = %+v, %v; want default OFF", got, err)
	}
}

func TestCaffeinateCheckpointFailedConsumeCannotRestoreAfterAccessRecovers(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permission enforcement for the current user")
	}
	directory := t.TempDir()
	previous := NewCaffeinateSessionStoreForWorker(directory, "key7", "", "worker-1")
	if err := previous.Save(CaffeinateIntent{RequestedEnabled: true}); err != nil {
		t.Fatal(err)
	}
	current := NewCaffeinateSessionStoreForWorker(directory, "key7", "worker-1", "worker-2")
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0700)
	got, err := current.Load()
	if err == nil || got != (CaffeinateIntent{}) {
		t.Fatalf("failed consume = %+v, %v; want OFF and error", got, err)
	}
	// This worker cannot persist OFF while the directory is unwritable, so
	// the old ON checkpoint is still present when the next worker starts.
	if err := current.Save(CaffeinateIntent{}); err == nil {
		t.Fatal("expected OFF checkpoint write failure")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	next := NewCaffeinateSessionStoreForWorker(directory, "key7", "worker-2", "worker-3")
	got, err = next.Load()
	if err != nil || got != (CaffeinateIntent{}) {
		t.Fatalf("load after access recovery = %+v, %v; stale ON must remain invalid", got, err)
	}
}
