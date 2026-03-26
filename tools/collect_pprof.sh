#!/usr/bin/env bash

set -uo pipefail

ADDR="${MY_STREAMDECK_PPROF_ADDR:-127.0.0.1:6060}"
INTERVAL_SECONDS="${PPROF_INTERVAL_SECONDS:-600}"
DURATION_SECONDS="${PPROF_DURATION_SECONDS:-86400}"
OUT_DIR="${PPROF_OUTPUT_DIR:-$HOME/my-streamdeck-pprof-$(date +%Y%m%d-%H%M%S)}"

usage() {
  cat <<'EOF'
Usage: collect_pprof.sh

Environment variables:
  MY_STREAMDECK_PPROF_ADDR   pprof listen address. Default: 127.0.0.1:6060
  PPROF_INTERVAL_SECONDS     sample interval. Default: 600
  PPROF_DURATION_SECONDS     total run length. Default: 86400
  PPROF_OUTPUT_DIR           output directory
EOF
}

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

lookup_pid() {
  local port
  port="${ADDR##*:}"
  lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | head -n 1 || true
}

record_pid_metadata() {
  local pid="$1"
  local binary_path=""

  {
    printf 'timestamp=%s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')"
    printf 'pid=%s\n' "$pid"
    ps -o pid=,ppid=,rss=,vsz=,etime=,command= -p "$pid"
  } >> "$OUT_DIR/pid.log"
  printf -- '---\n' >> "$OUT_DIR/pid.log"

  binary_path="$(lsof -a -p "$pid" -d txt -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1 || true)"
  if [[ -n "$binary_path" && -x "$binary_path" ]]; then
    go version -m "$binary_path" > "$OUT_DIR/binary-$pid.version.txt" 2>&1 || true
  fi
}

capture_profile() {
  local name="$1"
  local suffix="$2"
  local sample_index="${3:-}"
  local url="$4"
  local out="$OUT_DIR/$name-$suffix"
  local top_out="$OUT_DIR/$name-${suffix%.pb.gz}.top.txt"

  if ! curl -fsS "$url" -o "$out"; then
    log "capture failed: $url"
    return
  fi

  case "$name" in
    heap)
      go tool pprof -top -sample_index="${sample_index}" "$out" > "$top_out" 2>&1 || true
      ;;
    allocs)
      go tool pprof -top -sample_index="${sample_index}" "$out" > "$top_out" 2>&1 || true
      ;;
    threadcreate)
      go tool pprof -top "$out" > "$top_out" 2>&1 || true
      ;;
  esac
}

main() {
  local iterations
  local current_pid=""
  local i
  local ts
  local pid

  if [[ "${1:-}" == "--help" ]]; then
    usage
    return 0
  fi

  if ! [[ "$INTERVAL_SECONDS" =~ ^[0-9]+$ && "$INTERVAL_SECONDS" -gt 0 ]]; then
    log "invalid PPROF_INTERVAL_SECONDS: $INTERVAL_SECONDS"
    return 1
  fi
  if ! [[ "$DURATION_SECONDS" =~ ^[0-9]+$ && "$DURATION_SECONDS" -gt 0 ]]; then
    log "invalid PPROF_DURATION_SECONDS: $DURATION_SECONDS"
    return 1
  fi

  mkdir -p "$OUT_DIR"
  iterations=$(( (DURATION_SECONDS + INTERVAL_SECONDS - 1) / INTERVAL_SECONDS ))

  {
    printf 'started_at=%s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')"
    printf 'addr=%s\n' "$ADDR"
    printf 'interval_seconds=%s\n' "$INTERVAL_SECONDS"
    printf 'duration_seconds=%s\n' "$DURATION_SECONDS"
    printf 'iterations=%s\n' "$iterations"
    printf 'cwd=%s\n' "$(pwd)"
  } > "$OUT_DIR/meta.txt"

  curl -fsS "http://$ADDR/debug/pprof/cmdline" -o "$OUT_DIR/cmdline.txt" 2>/dev/null || true

  for ((i = 0; i < iterations; i++)); do
    ts="$(date +%Y%m%d-%H%M%S)"
    pid="$(lookup_pid)"

    if [[ -z "$pid" ]]; then
      log "no process is listening on $ADDR"
      {
        printf 'missing_pid_at=%s\n' "$ts"
        printf -- '---\n'
      } >> "$OUT_DIR/pid.log"
    else
      if [[ "$pid" != "$current_pid" ]]; then
        current_pid="$pid"
        log "tracking pid=$current_pid"
        record_pid_metadata "$current_pid"
      fi
      ps -o pid=,ppid=,rss=,vsz=,etime=,command= -p "$current_pid" >> "$OUT_DIR/rss.log"
      printf -- '--- %s\n' "$ts" >> "$OUT_DIR/rss.log"
    fi

    capture_profile "heap" "$ts.pb.gz" "inuse_space" "http://$ADDR/debug/pprof/heap?gc=1"
    capture_profile "allocs" "$ts.pb.gz" "alloc_space" "http://$ADDR/debug/pprof/allocs"
    capture_profile "threadcreate" "$ts.pb.gz" "" "http://$ADDR/debug/pprof/threadcreate"
    curl -fsS "http://$ADDR/debug/pprof/goroutine?debug=2" -o "$OUT_DIR/goroutine-$ts.txt" 2>/dev/null || log "capture failed: goroutine-$ts"

    if (( i + 1 < iterations )); then
      sleep "$INTERVAL_SECONDS"
    fi
  done

  printf 'finished_at=%s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')" >> "$OUT_DIR/meta.txt"
  log "collection finished: $OUT_DIR"
}

main "$@"
