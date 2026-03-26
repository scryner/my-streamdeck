OUT="$HOME/my-streamdeck-pprof-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$OUT"

PID=$(lsof -nP -iTCP:6060 -sTCP:LISTEN -t | head -n 1)
echo "pid=$PID" | tee "$OUT/meta.txt"

for i in $(seq 0 143); do
  TS=$(date +%Y%m%d-%H%M%S)

  ps -o pid=,ppid=,rss=,vsz=,etime=,command= -p "$PID" >> "$OUT/rss.log"
  echo "--- $TS" >> "$OUT/rss.log"

  curl -s "http://127.0.0.1:6060/debug/pprof/heap?gc=1" -o "$OUT/heap-$TS.pb.gz"
  curl -s "http://127.0.0.1:6060/debug/pprof/allocs" -o "$OUT/allocs-$TS.pb.gz"
  curl -s "http://127.0.0.1:6060/debug/pprof/goroutine?debug=2" -o "$OUT/goroutine-$TS.txt"

  go tool pprof -top -sample_index=inuse_space "$OUT/heap-$TS.pb.gz" > "$OUT/heap-$TS.top.txt" 2>&1
  go tool pprof -top -sample_index=alloc_space "$OUT/allocs-$TS.pb.gz" > "$OUT/allocs-$TS.top.txt" 2>&1

  sleep 600
done
