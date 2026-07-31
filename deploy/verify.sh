#!/usr/bin/env bash
# Smoke test against a running Sitepass instance.
# Usage: SITEPASS_API=https://sitepass.tech ./deploy/verify.sh
set -euo pipefail

API="${SITEPASS_API:-http://127.0.0.1:8080}"
API="${API%/}"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

echo "==> health"
health="$(curl -fsS "${API}/api/v1/health")"
echo "${health}" | grep -q '"ok":true' || { echo "health not ok: ${health}"; exit 1; }

echo "==> create token"
create="$(curl -fsS -X POST "${API}/api/v1/tokens" -H 'Content-Type: application/json' -d '{}')"
token="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])' <<<"${create}")"
preview="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["preview_url"])' <<<"${create}")"
[[ -n "${token}" && -n "${preview}" ]] || { echo "bad create: ${create}"; exit 1; }
echo "token ok; preview=${preview}"

echo "==> upload fixture"
printf '<!DOCTYPE html><html><body><h1>sitepass-verify</h1></body></html>\n' > "${TMP}/index.html"
tar -C "${TMP}" -czf "${TMP}/site.tar.gz" index.html
upload="$(curl -fsS -X POST "${API}/api/v1/upload" \
  -H "Authorization: Bearer ${token}" \
  -H 'Content-Type: application/octet-stream' \
  --data-binary @"${TMP}/site.tar.gz")"
echo "${upload}" | grep -q '"ok":true' || { echo "upload failed: ${upload}"; exit 1; }

echo "==> status"
status="$(curl -fsS "${API}/api/v1/status" -H "Authorization: Bearer ${token}")"
echo "${status}" | grep -q '"has_build":true' || { echo "status missing build: ${status}"; exit 1; }

# Preview fetch may need public DNS/TLS; skip if SITEPASS_SKIP_PREVIEW=1
if [[ "${SITEPASS_SKIP_PREVIEW:-0}" != "1" ]]; then
  echo "==> preview fetch ${preview}"
  body="$(curl -fsS --max-time 30 "${preview}/")"
  echo "${body}" | grep -q 'sitepass-verify' || { echo "preview content mismatch"; exit 1; }
fi

echo "==> delete token"
code="$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE "${API}/api/v1/token" -H "Authorization: Bearer ${token}")"
[[ "${code}" == "204" ]] || { echo "delete returned ${code}"; exit 1; }

echo "==> status after delete"
code="$(curl -sS -o /dev/null -w '%{http_code}' "${API}/api/v1/status" -H "Authorization: Bearer ${token}")"
[[ "${code}" == "410" || "${code}" == "404" ]] || { echo "expected gone/not found, got ${code}"; exit 1; }

echo "verify OK"
