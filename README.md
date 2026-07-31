# Sitepass

Temporary public URLs for static site builds. Create a token, hand the
instruction to an agent, upload a build over HTTP.

## Documents

- [Specification](docs/SPECIFICATION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Design contract](docs/DESIGN_CONTRACT.md)
- [Code contract](docs/CODE_CONTRACT.md)
- Agent contract: [`/llms.txt`](llms.txt)

## Status

MVP on Debian with Caddy, PostgreSQL, and systemd. Optional accounts
(username + password, no email). Anonymous tokens still work.

## Self-hosting

```bash
git clone https://github.com/posthut/sitepass.git /opt/sitepass
cd /opt/sitepass
sudo mkdir -p /etc/sitepass
sudo cp deploy/sitepass.env.example /etc/sitepass/sitepass.env
sudo editor /etc/sitepass/sitepass.env
sudo ./deploy/bootstrap.sh
```

Control and preview may be **different registrable domains**, or the
**same hostname** for shared-apex mode (`SITEPASS_CONTROL_DOMAIN` =
`SITEPASS_PREVIEW_DOMAIN`).

Smoke test against a running instance:

```bash
SITEPASS_API=https://your.control.domain make verify
```

Simulate a clean second-machine install on the same host (does **not**
overwrite production Caddy/systemd):

```bash
sudo ./deploy/fresh-smoke.sh
```
