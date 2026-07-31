#!/usr/bin/env bash
# Sitepass self-host bootstrap (idempotent install/upgrade).
set -euo pipefail

SITEPASS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${SITEPASS_ENV_FILE:-/etc/sitepass/sitepass.env}"
DEPLOY_DIR="${SITEPASS_ROOT}/deploy"
TEMPLATE_DIR="${DEPLOY_DIR}/templates"
MIN_FREE_MB="${SITEPASS_MIN_FREE_MB:-2048}"

step() {
	printf '\n==> %s\n' "$*"
}

die() {
	printf 'ERROR: %s\n' "$*" >&2
	exit 1
}

require_root() {
	if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
		die "Run as root (sudo ./deploy/bootstrap.sh)."
	fi
}

load_env() {
	step "Loading environment from ${ENV_FILE}"
	[[ -f "${ENV_FILE}" ]] || die "Missing ${ENV_FILE}. Copy deploy/sitepass.env.example and edit it first."
	chmod 600 "${ENV_FILE}"
	# shellcheck disable=SC1090
	set -a
	source "${ENV_FILE}"
	set +a

	CONTROL_DOMAIN="${SITEPASS_CONTROL_DOMAIN:-}"
	PREVIEW_DOMAIN="${SITEPASS_PREVIEW_DOMAIN:-}"
	BUILDS_DIR="${SITEPASS_BUILDS_DIR:-/srv/sitepass/builds}"
	ABUSE_CONTACT="${SITEPASS_ABUSE_CONTACT:-}"
	LISTEN_ADDR="${SITEPASS_LISTEN:-127.0.0.1:8080}"
	DB_DSN="${SITEPASS_DB_DSN:-}"

	[[ -n "${CONTROL_DOMAIN}" ]] || die "SITEPASS_CONTROL_DOMAIN is required in ${ENV_FILE}."
	[[ -n "${PREVIEW_DOMAIN}" ]] || die "SITEPASS_PREVIEW_DOMAIN is required in ${ENV_FILE}."
	[[ -n "${ABUSE_CONTACT}" ]] || die "SITEPASS_ABUSE_CONTACT is required in ${ENV_FILE}."
	[[ -n "${DB_DSN}" ]] || die "SITEPASS_DB_DSN is required in ${ENV_FILE}."
}

registrable_suffix() {
	local host="${1,,}"
	host="${host// /}"
	local IFS='.'
	local -a parts
	read -ra parts <<< "${host}"
	local n="${#parts[@]}"
	if (( n < 2 )); then
		printf '%s' "${host}"
		return
	fi
	printf '%s.%s' "${parts[n-2]}" "${parts[n-1]}"
}

validate_host() {
	local host="$1"
	[[ "${host}" == *.* ]] || die "Invalid hostname (need at least one dot): ${host}"
}

validate_domains() {
	step "Validating control and preview domains"
	validate_host "${CONTROL_DOMAIN}"
	validate_host "${PREVIEW_DOMAIN}"

	local ctrl_suffix prev_suffix
	ctrl_suffix="$(registrable_suffix "${CONTROL_DOMAIN}")"
	prev_suffix="$(registrable_suffix "${PREVIEW_DOMAIN}")"

	if [[ "${CONTROL_DOMAIN}" == "${PREVIEW_DOMAIN}" ]]; then
		printf 'Shared-apex mode: control and preview use %s\n' "${CONTROL_DOMAIN}"
	elif [[ "${ctrl_suffix}" == "${prev_suffix}" ]]; then
		die "SITEPASS_CONTROL_DOMAIN and SITEPASS_PREVIEW_DOMAIN share registrable suffix ${ctrl_suffix}. Use different registrable domains, or set both to the same hostname for shared-apex mode."
	else
		printf 'Split-domain mode: control=%s preview=%s\n' "${CONTROL_DOMAIN}" "${PREVIEW_DOMAIN}"
	fi
}

check_platform() {
	step "Checking platform (Debian amd64, disk space)"
	[[ "$(uname -m)" == "x86_64" ]] || die "Unsupported architecture $(uname -m); amd64/x86_64 required."
	[[ -f /etc/debian_version ]] || die "Unsupported OS: expected Debian (missing /etc/debian_version)."

	local free_mb
	free_mb="$(df -BM / | awk 'NR==2 { sub(/M$/, "", $4); print $4 }')"
	if [[ -z "${free_mb}" ]] || (( free_mb < MIN_FREE_MB )); then
		die "Insufficient free space on / (need at least ${MIN_FREE_MB} MiB, have ${free_mb:-0} MiB)."
	fi
	printf 'OK: Debian %s, %s MiB free on /\n' "$(cut -d. -f1 /etc/debian_version)" "${free_mb}"
}

ensure_apt_packages() {
	step "Installing system packages (postgresql, caddy, curl, build toolchain)"
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq
	local pkgs=(postgresql caddy curl ca-certificates)
	# Build from source in the git tree.
	pkgs+=(golang-go nodejs npm git make)
	apt-get install -y -qq "${pkgs[@]}"
}

ensure_service_user() {
	step "Ensuring service OS user"
	if [[ "${SITEPASS_ROOT}" == /home/* ]]; then
		SERVICE_USER="$(stat -c '%U' "${SITEPASS_ROOT}")"
		SERVICE_GROUP="$(stat -c '%G' "${SITEPASS_ROOT}")"
		printf 'Using clone owner %s:%s (tree under /home)\n' "${SERVICE_USER}" "${SERVICE_GROUP}"
	else
		SERVICE_USER="${SITEPASS_SERVICE_USER:-sitepass}"
		SERVICE_GROUP="${SITEPASS_SERVICE_GROUP:-sitepass}"
		if ! id "${SERVICE_USER}" &>/dev/null; then
			useradd --system --home /nonexistent --shell /usr/sbin/nologin "${SERVICE_USER}"
			printf 'Created system user %s\n' "${SERVICE_USER}"
		fi
	fi
}

ensure_directories() {
	step "Preparing /srv/sitepass paths"
	mkdir -p "${BUILDS_DIR}" /srv/sitepass/system
	chmod 755 /srv/sitepass "${BUILDS_DIR}" /srv/sitepass/system

	if [[ -d "${DEPLOY_DIR}/system" ]]; then
		cp -a "${DEPLOY_DIR}/system/." /srv/sitepass/system/
	fi

	if [[ "${SERVICE_USER}" != root ]]; then
		chown -R "${SERVICE_USER}:${SERVICE_GROUP}" /srv/sitepass
	fi
}

dsn_has_password() {
	local dsn="$1"
	case "${dsn}" in
		postgres://*:*@*)
			return 0
			;;
		postgresql://*:*@*)
			return 0
			;;
	esac
	return 1
}

dsn_uses_tcp_with_user() {
	local dsn="$1"
	case "${dsn}" in
		postgres://*@*|postgresql://*@*)
			return 0
			;;
	esac
	return 1
}

update_env_dsn_password() {
	local new_pass="$1"
	local tmp
	tmp="$(mktemp)"
	awk -v pass="${new_pass}" '
		/^SITEPASS_DB_DSN=/ {
			line = $0
			sub(/^SITEPASS_DB_DSN=/, "", line)
			if (line ~ /^postgres(ql)?:\/\/[^:@]+@/) {
				sub(/^(postgres(ql)?:\/\/[^:@]+)@/, "\\1:" pass "@", line)
				print "SITEPASS_DB_DSN=" line
				next
			}
		}
		{ print }
	' "${ENV_FILE}" > "${tmp}"
	chmod 600 "${tmp}"
	mv "${tmp}" "${ENV_FILE}"
	DB_DSN="$(grep '^SITEPASS_DB_DSN=' "${ENV_FILE}" | cut -d= -f2-)"
}

ensure_postgres() {
	step "Ensuring PostgreSQL role and database sitepass"
	systemctl enable --now postgresql >/dev/null 2>&1 || true

	local role_exists db_exists
	role_exists="$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='sitepass'" 2>/dev/null | tr -d '[:space:]')"
	db_exists="$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='sitepass'" 2>/dev/null | tr -d '[:space:]')"

	local new_pass=""
	if dsn_uses_tcp_with_user "${DB_DSN}" && ! dsn_has_password "${DB_DSN}"; then
		new_pass="$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)"
		printf 'Generated database password (DSN had user but no password).\n'
	fi

	if [[ "${role_exists}" != "1" ]]; then
		if [[ -n "${new_pass}" ]]; then
			sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE ROLE sitepass LOGIN PASSWORD '${new_pass}';"
		else
			sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE ROLE sitepass LOGIN;"
		fi
		printf 'Created PostgreSQL role sitepass\n'
	elif [[ -n "${new_pass}" ]]; then
		sudo -u postgres psql -v ON_ERROR_STOP=1 -c "ALTER ROLE sitepass PASSWORD '${new_pass}';"
		printf 'Set password on existing PostgreSQL role sitepass\n'
	else
		printf 'PostgreSQL role sitepass already exists (unchanged)\n'
	fi

	if [[ "${db_exists}" != "1" ]]; then
		sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE sitepass OWNER sitepass;"
		printf 'Created database sitepass\n'
	else
		printf 'Database sitepass already exists\n'
	fi

	if [[ -n "${new_pass}" ]]; then
		update_env_dsn_password "${new_pass}"
		printf 'Updated SITEPASS_DB_DSN in %s with generated password\n' "${ENV_FILE}"
	fi
}

build_application() {
	step "Building Go API (${SITEPASS_ROOT}/bin/sitepass)"
	export GOCACHE="${GOCACHE:-/var/cache/sitepass/go-build}"
	export GOTMPDIR="${GOTMPDIR:-/var/tmp/sitepass-go}"
	mkdir -p "${GOCACHE}" "${GOTMPDIR}"
	chmod 1777 /var/tmp/sitepass-go 2>/dev/null || true

	if [[ "${SERVICE_USER}" != root ]]; then
		chown -R "${SERVICE_USER}:${SERVICE_GROUP}" "${GOCACHE}" "${GOTMPDIR}" 2>/dev/null || true
	fi

	(
		cd "${SITEPASS_ROOT}"
		if [[ "${SERVICE_USER}" != root ]] && [[ "$(id -un)" == root ]]; then
			sudo -u "${SERVICE_USER}" env GOCACHE="${GOCACHE}" GOTMPDIR="${GOTMPDIR}" \
				make build
		else
			make build
		fi
	)
	[[ -x "${SITEPASS_ROOT}/bin/sitepass" ]] || die "Go build did not produce ${SITEPASS_ROOT}/bin/sitepass"

	step "Building control site (npm)"
	(
		cd "${SITEPASS_ROOT}/web"
		if [[ "${SERVICE_USER}" != root ]] && [[ "$(id -un)" == root ]]; then
			sudo -u "${SERVICE_USER}" npm ci
			sudo -u "${SERVICE_USER}" npm run build
		else
			npm ci
			npm run build
		fi
	)
	[[ -d "${SITEPASS_ROOT}/web/dist" ]] || die "Web build did not produce web/dist"

	if [[ "${SITEPASS_ROOT}" != /home/* ]] && [[ "${SERVICE_USER}" != root ]]; then
		chown -R "${SERVICE_USER}:${SERVICE_GROUP}" "${SITEPASS_ROOT}"
	fi
}

render_template() {
	local src="$1" dest="$2"
	[[ -f "${src}" ]] || die "Missing template ${src}"
	sed \
		-e "s|{{CONTROL_DOMAIN}}|${CONTROL_DOMAIN}|g" \
		-e "s|{{PREVIEW_DOMAIN}}|${PREVIEW_DOMAIN}|g" \
		-e "s|{{ABUSE_CONTACT}}|${ABUSE_CONTACT}|g" \
		-e "s|{{BUILDS_DIR}}|${BUILDS_DIR}|g" \
		-e "s|{{SITEPASS_ROOT}}|${SITEPASS_ROOT}|g" \
		-e "s|{{ENV_FILE}}|${ENV_FILE}|g" \
		-e "s|{{SERVICE_USER}}|${SERVICE_USER}|g" \
		-e "s|{{SERVICE_GROUP}}|${SERVICE_GROUP}|g" \
		"${src}" > "${dest}"
}

install_systemd_unit() {
	step "Installing systemd unit sitepass.service"
	render_template "${TEMPLATE_DIR}/sitepass.service" /etc/systemd/system/sitepass.service
	systemctl daemon-reload
	systemctl enable sitepass.service
}

install_caddyfile() {
	step "Rendering Caddyfile"
	render_template "${TEMPLATE_DIR}/Caddyfile" /etc/caddy/Caddyfile
	if command -v caddy >/dev/null; then
		caddy validate --config /etc/caddy/Caddyfile
	fi
	systemctl enable caddy.service >/dev/null 2>&1 || true
}

reload_services() {
	step "Reloading sitepass and Caddy"
	systemctl restart sitepass.service
	systemctl reload caddy.service 2>/dev/null || systemctl restart caddy.service
}

smoke_health() {
	step "Smoke test: GET /api/v1/health"
	local url="http://${LISTEN_ADDR}/api/v1/health"
	local attempt body
	for attempt in 1 2 3 4 5 6 7 8 9 10; do
		if body="$(curl -fsS "${url}" 2>/dev/null)"; then
			if grep -q '"status"' <<< "${body}"; then
				printf 'Health OK: %s\n' "${body}"
				return 0
			fi
			die "Health endpoint returned unexpected body: ${body}"
		fi
		sleep 1
	done
	die "Health check failed after 10 attempts (${url}). Check: journalctl -u sitepass -n 50"
}

main() {
	require_root
	load_env
	check_platform
	validate_domains
	ensure_apt_packages
	ensure_service_user
	ensure_directories
	ensure_postgres
	build_application
	install_systemd_unit
	install_caddyfile
	reload_services
	smoke_health
	step "Bootstrap finished successfully"
}

main "$@"
