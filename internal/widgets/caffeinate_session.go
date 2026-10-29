package widgets

import "fmt"

// WindowServer omits CGSSessionScreenIsLocked while unlocked. Absence is valid
// only in an otherwise valid, logged-in console session; null dictionaries,
// malformed values and inactive sessions must never enable assertions.
func caffeinateSessionLocked(available bool, onConsole, loginDone, screenLocked int) (bool, error) {
	if !available || screenLocked < -1 || screenLocked > 1 || onConsole < 0 || loginDone < 0 {
		return false, fmt.Errorf("unavailable or malformed WindowServer session")
	}
	if screenLocked == 1 {
		return true, nil
	}
	if onConsole != 1 || loginDone != 1 {
		return false, fmt.Errorf("no active logged-in console session")
	}
	return false, nil
}
