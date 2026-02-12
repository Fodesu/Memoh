# Memoh Deployment Guide

## Quick Deploy

```bash
git clone https://github.com/memohai/Memoh.git
cd Memoh
./deploy.sh
```

Access:
- Web UI: http://localhost
- API: http://localhost:8080
- Agent: http://localhost:8081

Default credentials: `admin` / `admin123`

## Manual Deploy

```bash
cp docker/config/config.docker.toml config.toml
nano config.toml  # Change passwords and secrets
nerdctl build -f docker/Dockerfile.mcp -t memoh-mcp:latest .
docker compose up -d
```

## Required Configuration

Must change in `config.toml`:
- `admin.password` - Admin password
- `auth.jwt_secret` - JWT secret (generate with `openssl rand -base64 32`)
- `postgres.password` - Database password

## Common Commands

```bash
docker compose up -d          # Start
docker compose down           # Stop
docker compose logs -f        # View logs
nerdctl ps -a | grep memoh-bot # View bot containers
```

## Production

1. Configure HTTPS (create `docker-compose.override.yml` with SSL certs)
2. Change all default passwords
3. Configure firewall
4. Set resource limits
5. Regular backups

## Troubleshooting

```bash
docker compose logs server    # View service logs
docker compose config         # Check configuration
docker compose build --no-cache && docker compose up -d  # Rebuild
```

## Security Warnings

⚠️ Main service has host Docker access - only run in trusted environments
⚠️ Must change all default passwords and secrets
⚠️ Use HTTPS in production

