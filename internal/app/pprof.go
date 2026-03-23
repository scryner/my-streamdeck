package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"
)

const pprofAddrEnv = "MY_STREAMDECK_PPROF_ADDR"

func startPprofServerFromEnv() (func(context.Context) error, error) {
	raw := strings.TrimSpace(os.Getenv(pprofAddrEnv))
	if raw == "" {
		return nil, nil
	}

	addr, err := normalizePprofAddr(raw)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/cmdline", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/profile", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/symbol", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/trace", http.DefaultServeMux.ServeHTTP)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("pprof listening on http://%s/debug/pprof/", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("pprof server stopped: %v", err)
		}
	}()

	return srv.Shutdown, nil
}

func normalizePprofAddr(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%s is empty", pprofAddrEnv)
	}

	host, port, err := net.SplitHostPort(raw)
	if err == nil {
		if _, err := strconv.Atoi(port); err != nil {
			return "", fmt.Errorf("invalid %s value %q", pprofAddrEnv, raw)
		}
		if host == "" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, port), nil
	}

	if _, _, err := net.SplitHostPort(":" + raw); err == nil {
		if _, err := strconv.Atoi(raw); err != nil {
			return "", fmt.Errorf("invalid %s value %q", pprofAddrEnv, raw)
		}
		return net.JoinHostPort("127.0.0.1", raw), nil
	}

	return "", fmt.Errorf("invalid %s value %q", pprofAddrEnv, raw)
}
