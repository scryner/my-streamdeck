package app

import (
	"log"
	"sync/atomic"
)

var verboseLogging atomic.Bool

func SetVerboseLogging(enabled bool) {
	verboseLogging.Store(enabled)
}

func debugf(format string, args ...any) {
	if verboseLogging.Load() {
		log.Printf(format, args...)
	}
}
