#!/usr/bin/env bash
# Temporary daemon only; never installs a slot or writes the root filesystem.
# ECHO_HOST=root@<address> ECHO_KEY=<private-key-path> [ECHO_KNOWN_HOSTS=<file>] tools/dev-push.sh [--revert]
set -euo pipefail
case "${1:-}" in
  '') mode=push ;;
  --revert) mode=revert ;;
  *) echo "Usage: $0 [--revert]" >&2; exit 2 ;;
esac
[[ $# -le 1 ]] || exit 2
: "${ECHO_HOST:?Set ECHO_HOST (root@address)}" "${ECHO_KEY:?Set ECHO_KEY}"
[[ $ECHO_HOST != -* ]] || { echo 'ECHO_HOST must not start with -' >&2; exit 2; }
opts=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=yes -i "$ECHO_KEY")
[[ -z ${ECHO_KNOWN_HOSTS:-} ]] || opts+=(-o "UserKnownHostsFile=$ECHO_KNOWN_HOSTS")
root=$(cd "$(dirname "$0")/.." && pwd)
version=''
work=''
release_lock=no
cleanup() {
  [[ -z $work ]] || rm -rf "$work"
  if [[ $release_lock == yes ]]; then
    ssh "${opts[@]}" "$ECHO_HOST" 'rmdir /run/techo5/dev-push.lock'
  fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
if [[ $mode == push ]]; then
  work=$(mktemp -d "${TMPDIR:-/tmp}/techo5-dev.XXXXXX")
  commit=$(git -C "$root" rev-parse --short=8 HEAD)
  version="v0.0.0-t94.test.$commit"
  pkg=github.com/HuskerMinion/techo5/echod/internal/layout
  (cd "$root/echod" && GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 "${GO:-go}" build -trimpath \
    -ldflags "-s -w -X $pkg.Version=$version -X $pkg.GitCommit=$commit -X $pkg.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o "$work/echod" ./cmd/echod)
fi
# Atomic lock covers preflight, upload, daemon replacement and revert.
ssh "${opts[@]}" "$ECHO_HOST" sh -s -- "$mode" <<'PREFLIGHT'
set -eu
lock=/run/techo5/dev-push.lock
mkdir "$lock" || { echo 'Another dev-push operation holds the lock' >&2; exit 1; }
trap 'rmdir "$lock"' EXIT
trap 'exit 1' HUP INT TERM
if [ "$1" = push ]; then
  if grep -q ' /usr/local/bin/techo5 ' /proc/mounts; then
    echo 'Test mount already exists; run --revert first' >&2; exit 1
  fi
  test ! -e /tmp/echod-test
  test ! -e /tmp/echod-test.original-version
  test ! -e /run/techo5/hold
fi
trap - EXIT HUP INT TERM
PREFLIGHT
release_lock=yes
if [[ $mode == push ]]; then
  scp -O "${opts[@]}" "$work/echod" "$ECHO_HOST:/tmp/echod-test"
fi
# The remote EXIT handler now owns the lock, including on HUP/TERM.
# If SSH never starts the handler, a stale lock blocks unsafe further pushes.
release_lock=no
ssh "${opts[@]}" "$ECHO_HOST" sh -s -- "$mode" "$version" <<'REMOTE'
set -eu
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
bin=/usr/local/bin/techo5
saved=/tmp/echod-test.original-version
lock=/run/techo5/dev-push.lock
rollback=no
cleanup() {
  rc=$?
  trap - EXIT
  # Signals mark failure, but must not skip the final lock release.
  trap 'rc=1' HUP INT TERM
  set +e
  if [ "$rollback" = yes ]; then
    if mounted; then
      echo 'Rolling back failed test; if SSH fails, run --revert immediately' >&2
      # Keep errexit inside rollback, not in this final cleanup. Do not use
      # `revert || ...`: that disables its fail-fast guards in POSIX shells.
      (set -e; revert)
      rollback_rc=$?
      if [ "$rollback_rc" -ne 0 ]; then
        echo 'Automatic rollback failed; run --revert immediately' >&2
        if [ "$rc" -eq 0 ]; then rc=$rollback_rc; fi
      fi
    else
      rm -f /tmp/echod-test "$saved"
    fi
  fi
  rmdir "$lock"
  lock_rc=$?
  if [ "$rc" -eq 0 ]; then rc=$lock_rc; fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
mounted() { grep -q ' /usr/local/bin/techo5 ' /proc/mounts; }
wait_new() {
  n=0
  while [ "$n" -lt 30 ]; do
    pid=$(pidof techo5 || true)
    if [ -n "$pid" ] && [ "$pid" != "$old" ] && [ -e "/proc/$pid/exe" ] && cmp -s "$bin" "/proc/$pid/exe"; then
      echo "PID=$pid executable matches $bin"
      return 0
    fi
    sleep 1; n=$((n+1))
  done
  echo 'No matching new daemon within 30 seconds' >&2; return 1
}
revert() (
  if ! mounted && [ ! -e "$saved" ]; then
    rm -f /tmp/echod-test
    echo 'Nothing to revert (nichts zu tun)'
    exit 0
  fi
  test -s "$saved"
  old=$(pidof techo5 || true)
  if mounted; then
    # This kernel refuses umount while the executable is running (EBUSY).
    # Use the existing respawn hold, stop first, then unmount and resume.
    test ! -e /run/techo5/hold
    trap 'rm -f /run/techo5/hold' EXIT
    trap 'exit 1' HUP INT TERM
    touch /run/techo5/hold
    killall techo5 || true
    n=0
    while pidof techo5 >/dev/null; do
      n=$((n+1)); test "$n" -lt 30; sleep 1
    done
    umount "$bin"
    rm -f /run/techo5/hold
    trap - EXIT HUP INT TERM
    wait_new
  else
    # An interrupted attempt may already have unmounted; still verify the process.
    old=''; wait_new
  fi
  actual=$("$bin" --version 2>/dev/null)
  test "$actual" = "$(cat "$saved")"
  echo "ORIGINAL: $actual"
  rm -f /tmp/echod-test "$saved"
)
if [ "$1" = revert ]; then
  revert
else
  test ! -e /run/techo5/hold
  if mounted; then echo 'Test mount already exists; refusing push' >&2; exit 1; fi
  test ! -e "$saved"
  old=$(pidof techo5)
  "$bin" --version 2>/dev/null > "$saved"
  chmod 755 /tmp/echod-test
  # Failure after mounting must restore the original immediately.
  rollback=yes
  mount --bind /tmp/echod-test "$bin"
  killall techo5
  wait_new
  actual=$("$bin" --version 2>/dev/null)
  case "$actual" in "echod version $2 ("*) ;; *) echo "Wrong test version: $actual" >&2; exit 1 ;; esac
  echo "TEST: $actual"
  rollback=no
fi
REMOTE
