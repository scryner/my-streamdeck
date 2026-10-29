//go:build !darwin || !cgo

package app

import "time"

func pumpWorkerEvents() { time.Sleep(100 * time.Millisecond) }
