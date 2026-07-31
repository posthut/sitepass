# Sitepass

Temporary public URLs for static site builds. Create a token, hand it to an
agent, upload a build over HTTP.

## Documents

- [Specification](docs/SPECIFICATION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Design contract](docs/DESIGN_CONTRACT.md)
- [Code contract](docs/CODE_CONTRACT.md)
- Agent contract: [`/llms.txt`](llms.txt)

## Status

MVP scaffolding. Runtime packages are stubs; see `VERSION`.

## Self-hosting

```bash
git clone https://github.com/posthut/sitepass.git /opt/sitepass
cd /opt/sitepass
sudo cp deploy/sitepass.env.example /etc/sitepass/sitepass.env
sudo editor /etc/sitepass/sitepass.env
sudo ./deploy/bootstrap.sh
```

Control and preview domains must be **different registrable domains**.
