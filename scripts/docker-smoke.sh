#!/usr/bin/env bash
# Smoke-test the Docker image: runs as non-root, serves the UI and API, and keeps data across a restart.
# Usage: scripts/docker-smoke.sh [image] [port]
set -euo pipefail

image=${1:-meeting-room}
port=${2:-8080}
name=mr-smoke-$$
vol=$name-data
base=http://127.0.0.1:$port

cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; docker volume rm "$vol" >/dev/null 2>&1 || true; }
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; { docker logs "$name" 2>&1 | tail -20 >&2; } 2>/dev/null || true; exit 1; }

wait_up() {
  for _ in $(seq 1 30); do curl -sf "$base/rooms" >/dev/null && return 0; sleep 1; done
  fail "server did not come up on $base"
}

# Next Monday 10:00-11:00 Bangkok: always a weekday, in the future, within 60 days.
day=$(date -d 'next monday' +%F)
body='{"room_id":"r1","user_id":"smoke","start":"'$day'T10:00:00+07:00","end":"'$day'T11:00:00+07:00","attendees":2}'
post() { curl -s -o /dev/null -w '%{http_code}' -X POST "$base/bookings" -d "$body"; }

user=$(docker image inspect -f '{{.Config.User}}' "$image")
case "$user" in ""|0|root|0:*|root:*) fail "image runs as root (User='$user')";; esac
echo "ok: image user is '$user'"

docker run -d --name "$name" -p "$port:8080" -v "$vol:/data" "$image" >/dev/null
wait_up

[ "$(curl -s -o /dev/null -w '%{http_code}' "$base/")" = 200 ] || fail "GET / is not 200"
echo "ok: UI served"

[ "$(post)" = 201 ] || fail "first booking is not 201"
echo "ok: booking created"

docker restart "$name" >/dev/null
wait_up
[ "$(post)" = 409 ] || fail "booking did not survive restart (expected 409 on repeat)"
echo "ok: booking survived restart"
