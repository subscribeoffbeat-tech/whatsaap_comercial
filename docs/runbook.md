# WhatsApp Tool — Ops Runbook

## Quick reference

| Thing | Where |
|---|---|
| App binary | `/opt/whatsapptool/whatsapptool` |
| Config | `/opt/whatsapptool/.env` |
| Logs | `journalctl -u whatsapptool -f` |
| Health check | `curl http://localhost:8080/health` |
| Nginx config | `/etc/nginx/sites-available/whatsapptool` |
| DB backups | `/var/backups/whatsapptool/` |

---

## First-run setup

### 1 — Prerequisites

```bash
# PostgreSQL 15+
sudo apt install -y postgresql-15

# Create database and user
sudo -u postgres psql -c "CREATE USER waapp WITH PASSWORD 'changeme';"
sudo -u postgres psql -c "CREATE DATABASE whatsapptool OWNER waapp;"
```

### 2 — Build the binary (run on your dev machine)

From the project root on Windows (PowerShell):

```powershell
.\scripts\build.ps1
```

Or manually:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o whatsapptool ./cmd/server/
```

This produces a static Linux/amd64 binary with no external dependencies.

### 3 — Copy binary to VPS

```bash
mkdir -p /opt/whatsapptool
cp whatsapptool /opt/whatsapptool/
chmod +x /opt/whatsapptool/whatsapptool
```

### 4 — Create .env

```bash
cat > /opt/whatsapptool/.env <<EOF
DATABASE_URL=postgres://waapp:changeme@localhost/whatsapptool?sslmode=disable
PORT=8080
BASE_URL=https://yourdomain.com

# Generate with: openssl rand -hex 32
JWT_SECRET=replace-with-32-byte-hex-secret

# Meta Cloud API credentials
WA_PHONE_NUMBER_ID=1234567890
WA_WABA_ID=0987654321
WA_ACCESS_TOKEN=EAAxxxx...
WA_APP_SECRET=abcdef1234567890
WA_WEBHOOK_VERIFY_TOKEN=your-random-verify-token
EOF
chmod 600 /opt/whatsapptool/.env
```

### 5 — Systemd service

```ini
# /etc/systemd/system/whatsapptool.service
[Unit]
Description=WhatsApp Marketing Tool
After=network.target postgresql.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/whatsapptool
EnvironmentFile=/opt/whatsapptool/.env
ExecStart=/opt/whatsapptool/whatsapptool
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable --now whatsapptool
```

### 6 — Nginx reverse proxy

```nginx
server {
    listen 443 ssl;
    server_name yourdomain.com;

    ssl_certificate     /etc/letsencrypt/live/yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yourdomain.com/privkey.pem;

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Upgrade $http_upgrade;
        proxy_set_header   Connection "upgrade";
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_read_timeout 3600s;
    }
}
```

```bash
certbot --nginx -d yourdomain.com
nginx -t && systemctl reload nginx
```

### 7 — Meta webhook registration

In Meta Business Manager → WhatsApp → Configuration:
- Callback URL: `https://yourdomain.com/webhook`
- Verify token: value of `WA_WEBHOOK_VERIFY_TOKEN`
- Subscribe to: `messages`, `message_template_status_update`, `business_capability_update`

### 8 — First login

Browse to `https://yourdomain.com` — you'll be redirected to `/onboarding` to create the admin account.

---

## Day-to-day operations

### Deploy new version

```bash
systemctl stop whatsapptool
cp whatsapptool_new /opt/whatsapptool/whatsapptool
systemctl start whatsapptool
journalctl -u whatsapptool -n 50   # verify clean start
```

Migrations run automatically on startup. No manual `migrate` step needed.

### Check health

```bash
curl -s http://localhost:8080/health | python3 -m json.tool
# {"quality": "green", "status": "ok", "tier": "1000"}
```

503 = DB unreachable. Check `systemctl status postgresql`.

### View logs

```bash
journalctl -u whatsapptool -f                   # live tail
journalctl -u whatsapptool --since "1 hour ago" # last hour
```

### Manual backup

```bash
BACKUP_DIR=/var/backups/whatsapptool \
DATABASE_URL=$(grep DATABASE_URL /opt/whatsapptool/.env | cut -d= -f2-) \
/opt/whatsapptool/scripts/backup.sh
```

### Restore from backup

```bash
BACKUP=/var/backups/whatsapptool/whatsapptool_20240601_030000.sql.gz
gunzip -c "$BACKUP" | psql "$DATABASE_URL"
```

### Cron backup (add to /etc/cron.d/whatsapptool)

```
0 3 * * * www-data BACKUP_DIR=/var/backups/whatsapptool DATABASE_URL='postgres://...' /opt/whatsapptool/scripts/backup.sh >> /var/log/whatsapptool-backup.log 2>&1
```

---

## Troubleshooting

### Messages not sending

1. Check `journalctl -u whatsapptool -n 100` for Meta API errors.
2. Check quality rating in Settings. If RED, marketing sends are blocked by Meta.
3. Check daily cap: Settings → connection shows current tier.
4. If error 131049 (invalid number), that contact is permanently failed — normal.

### Webhook not receiving

1. Verify Meta webhook subscription is active (Meta Business Manager).
2. Check nginx is routing `/webhook` correctly.
3. Look for `X-Hub-Signature-256 mismatch` in logs — means `WA_APP_SECRET` is wrong.

### Database connection refused

```bash
systemctl status postgresql
sudo -u postgres psql -c "\l"   # list databases
```

### Out of disk (backups)

```bash
find /var/backups/whatsapptool -name '*.gz' -mtime +7 -delete
```

### Reset admin password

```bash
# Generate bcrypt hash for new password
htpasswd -bnBC 12 "" "newpassword" | tr -d ':\n'
# Then update in DB:
psql "$DATABASE_URL" -c "UPDATE agents SET password_hash='<hash>' WHERE email='admin@example.com';"
```

---

## Security checklist

- [ ] `.env` permissions: `chmod 600`
- [ ] `JWT_SECRET` is at least 32 random bytes
- [ ] `WA_ACCESS_TOKEN` stored only in `.env`, never in git
- [ ] Firewall: port 8080 not exposed publicly (nginx proxies)
- [ ] TLS enforced (nginx + certbot)
- [ ] Certbot auto-renewal: `systemctl status certbot.timer`
- [ ] Daily backups running: `crontab -l`
- [ ] Backup restore tested at least once

---

## Meta quality rating guide

| Rating | Meaning | Action |
|--------|---------|--------|
| GREEN | Good standing | Normal operation |
| YELLOW | Warning — too many blocks/spam | Reduce marketing frequency |
| RED | Restricted — marketing blocked | Stop campaigns, review opt-out rate |

Quality info is on the Settings page and in `/health`.
