#!/usr/bin/env bash
# Fresh-tree e2e on the same host without overwriting prod Caddy/systemd.
# Copies the current checkout (not a remote clone) so skip-flags are present.
set -euo pipefail

SRC_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="${SITEPASS_SMOKE_DIR:-/home/ai-agent/sitepass-fresh-smoke}"
LISTEN="${SITEPASS_SMOKE_LISTEN:-127.0.0.1:18080}"
DB_NAME="${SITEPASS_SMOKE_DB:-sitepass_fresh}"
ENV_FILE="${WORK}/smoke.env"
PID_FILE="${WORK}/sitepass.pid"

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
step() { printf '\n==> %s\n' "$*"; }

cleanup() {
	if [[ -f "${PID_FILE}" ]]; then
		kill "$(cat "${PID_FILE}")" 2>/dev/null || true
		rm -f "${PID_FILE}"
	fi
}
trap cleanup EXIT

[[ "$(id -u)" -eq 0 ]] || die "Run as root: sudo ./deploy/fresh-smoke.sh"

step "Clean workspace ${WORK}"
rm -rf "${WORK}"
mkdir -p "${WORK}/src"

step "Copy local tree ${SRC_ROOT} -> ${WORK}/src"
mkdir -p "${WORK}/src"
tar -C "${SRC_ROOT}" \
	--exclude='.git' \
	--exclude='.cache' \
	--exclude='bin' \
	--exclude='web/node_modules' \
	--exclude='web/dist' \
	--exclude='.data' \
	-cf - . | tar -C "${WORK}/src" -xf -

cd "${WORK}/src"

step "Create isolated env ${ENV_FILE}"
# Use peer/socket auth as OS user ai-agent via local trust is unreliable;
# reuse the existing sitepass role with password from production env if present.
PROD_ENV="${SITEPASS_PROD_ENV:-/etc/sitepass/sitepass.env}"
PASS=""
if [[ -f "${PROD_ENV}" ]]; then
	PROD_DSN="$(grep '^SITEPASS_DB_DSN=' "${PROD_ENV}" | cut -d= -f2- || true)"
	PASS="$(python3 -c 'import sys,urllib.parse; u=urllib.parse.urlparse(sys.argv[1]); print(urllib.parse.unquote(u.password or ""))' "${PROD_DSN}" 2>/dev/null || true)"
fi
if [[ -n "${PASS}" ]]; then
	DSN="postgres://sitepass:${PASS}@/${DB_NAME}?host=/var/run/postgresql"
else
	DSN="postgres://sitepass@/${DB_NAME}?host=/var/run/postgresql"
fi

cat > "${ENV_FILE}" <<EOF
SITEPASS_CONTROL_DOMAIN=sitepass.tech
SITEPASS_PREVIEW_DOMAIN=sitepass.tech
SITEPASS_BRAND_NAME=Sitepass
SITEPASS_LISTEN=${LISTEN}
SITEPASS_BUILDS_DIR=${WORK}/data/builds
SITEPASS_DB_DSN=${DSN}
SITEPASS_DEFAULT_LANGUAGE=en
SITEPASS_TOKEN_TTL_SECONDS=1800
SITEPASS_MAX_ARCHIVE_BYTES=104857600
SITEPASS_DISK_HIGH_WATER_PERCENT=80
SITEPASS_DISK_CRITICAL_PERCENT=90
SITEPASS_ABUSE_CONTACT=abuse@sitepass.tech
SITEPASS_READ_ONLY=false
SITEPASS_MAX_CONCURRENT_UPLOADS=2
SITEPASS_MIN_AVAILABLE_MEM_MB=64
SITEPASS_MIGRATIONS_DIR=migrations
SITEPASS_LLMS_PATH=llms.txt
SITEPASS_WEB_DIST=web/dist
EOF
chmod 600 "${ENV_FILE}"

step "Ensure database ${DB_NAME}"
sudo -u postgres psql -v ON_ERROR_STOP=1 -c "SELECT 1 FROM pg_roles WHERE rolname='sitepass'" | grep -q 1 \
	|| sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE ROLE sitepass LOGIN;"
if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'" | grep -q 1; then
	sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${DB_NAME} OWNER sitepass;"
fi

step "Bootstrap (skip apt/caddy/systemd — keep production intact)"
SITEPASS_ENV_FILE="${ENV_FILE}" \
SITEPASS_SKIP_APT=1 \
SITEPASS_SKIP_CADDY=1 \
SITEPASS_SKIP_SYSTEMD=1 \
SITEPASS_MIN_FREE_MB=512 \
	./deploy/bootstrap.sh

# Guard: production unit must still point at the live project.
if systemctl cat sitepass.service 2>/dev/null | grep -q 'sitepass-fresh-smoke'; then
	die "Refusing to continue: sitepass.service points at smoke tree"
fi

step "Start isolated binary on ${LISTEN}"
set -a
# shellcheck disable=SC1090
source "${ENV_FILE}"
set +a
mkdir -p "${SITEPASS_BUILDS_DIR}"
cd "${WORK}/src"
# Run as ai-agent when possible so peer paths match production ownership.
if id ai-agent &>/dev/null; then
	chown -R ai-agent:ai-agent "${WORK}"
	sudo -u ai-agent env $(grep -E '^SITEPASS_' "${ENV_FILE}" | xargs -d '\n') \
		./bin/sitepass >"${WORK}/sitepass.log" 2>&1 &
	echo $! > "${PID_FILE}"
else
	./bin/sitepass >"${WORK}/sitepass.log" 2>&1 &
	echo $! > "${PID_FILE}"
fi
sleep 2

step "Full verify against isolated instance"
SITEPASS_API="http://${LISTEN}" SITEPASS_SKIP_PREVIEW=1 ./deploy/verify.sh

step "Confirm production still healthy"
curl -fsS http://127.0.0.1:8080/api/v1/health >/dev/null
systemctl is-active sitepass.service >/dev/null

step "Fresh-tree smoke OK (production untouched)"
printf 'Smoke log: %s\n' "${WORK}/sitepass.log"
