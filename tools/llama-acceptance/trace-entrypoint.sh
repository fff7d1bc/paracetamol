#!/usr/bin/env bash
# Diagnostic only. The untouched normal entrypoint has already applied policy.
# Preserve server arguments and child environment. rocprofv3 observes captured
# execution, not just host submissions.
set -Eeuo pipefail
umask 077
out=$(mktemp -d /data/acceptance-trace-v1.XXXXXXXX)
printf 'ACCEPTANCE_TRACE_DIRECTORY=%s\n' "$out" >&2
/opt/venv/bin/rocprofv3 --version > "$out/profiler-version.txt" 2>&1
printf '%q ' "$@" > "$out/entrypoint-arguments.txt"
printf '\n' >> "$out/entrypoint-arguments.txt"
/opt/venv/bin/rocprofv3 --kernel-trace --output-format csv \
  --output-directory "$out" --output-file 'trace-%pid%' -- \
  /opt/trace/llama-server "$@" &
profiler=$!
wait_interrupted=0
forward() {
  wait_interrupted=1
  kill -s "$1" "$profiler" 2>/dev/null || true
}
trap 'forward TERM' TERM
trap 'forward INT' INT
trap 'forward HUP' HUP
status=0
while true; do
  wait_interrupted=0
  wait "$profiler" && status=0 || status=$?
  # A trap interrupts wait before the child finishes flushing. Retry even if
  # it has since exited: Bash retains its real status, unlike a kill -0 probe.
  (( wait_interrupted == 0 )) && break
done
printf '%s\n' "$status" > "$out/profiler-exit-status"
printf 'ACCEPTANCE_TRACE_COMPLETED=%s status=%s\n' "$out" "$status" >&2
exit "$status"
