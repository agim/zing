#!/bin/sh
# Starts the real Go app and a loopback-only mail inbox. Requires PostgreSQL.
set -eu
cd "$(dirname "$0")/.."
mkdir -p .lidza
zing_build=false
zing_configure=false
for zing_option in "$@"; do
  case "$zing_option" in
    --build) zing_build=true ;;
    --configure) zing_configure=true ;;
    *) echo 'Usage: ./scripts/local.sh [--configure] [--build]' >&2; exit 1 ;;
  esac
done
if [ "$zing_configure" = true ]; then
  if ! command -v lidza >/dev/null 2>&1; then
    echo 'Install the Lidza version pinned in go.mod before configuring a local profile.' >&2
    exit 1
  fi
  if [ ! -f config/master.key ] && [ -z "${LIDZA_MASTER_KEY:-}" ]; then lidza credentials init; fi
  LIDZA_MODE=dev lidza settings set APP_URL=http://127.0.0.1:3000 MAIL_PROVIDER=smtp MAIL_SMTP_URL=smtp://127.0.0.1:1025 MAIL_SMTP_SECURITY=none 'MAIL_FROM=Zing <noreply@zing.local>' STORAGE_PROVIDER=local ZING_ACCESS_MODE=private ZING_REQUIRE_SETUP=true
fi
if [ "$zing_build" = true ] || [ ! -x bin/zing ]; then
  if ! command -v lidza >/dev/null 2>&1; then
    echo 'Install the Lidza version pinned in go.mod, then run this command again.' >&2
    exit 1
  fi
  lidza build
fi
python3 scripts/local-mail.py > .lidza/local-mail.log 2>&1 &
zing_mail_pid=$!
zing_app_pid=
cleanup() {
  if [ -n "$zing_app_pid" ]; then kill "$zing_app_pid" 2>/dev/null || true; fi
  kill "$zing_mail_pid" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM
sleep 1
if ! kill -0 "$zing_mail_pid" 2>/dev/null; then
  echo 'Could not start local mail. Check .lidza/local-mail.log (ports 1025/8025).' >&2
  exit 1
fi
printf 'Zing: http://127.0.0.1:3000/\nLocal inbox: http://127.0.0.1:8025/\n'
LIDZA_MODE=dev AUTH_OWNER_CLAIM=true ./bin/zing &
zing_app_pid=$!
wait "$zing_app_pid"
