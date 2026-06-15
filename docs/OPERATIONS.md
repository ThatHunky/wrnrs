# Operations

## Local Docker Run

```bash
cp .env.example .env
docker compose up --build
```

The bot listens on port `8080`:

- `GET /healthz`
- `POST /telegram/webhook`

If `PUBLIC_BASE_URL` is empty, the app starts Telegram long polling. For production webhook mode, expose `/telegram/webhook`, set `TELEGRAM_WEBHOOK_SECRET`, and register the webhook with Telegram.

The default Compose binding is intentionally local-only and non-default:

```yaml
127.0.0.1:18087:8080
127.0.0.1:9000:9000
127.0.0.1:9001:9001
```

Caddy should reverse proxy `wrnrs.dobrovolskyi.com.ua` to `127.0.0.1:18087`.
MinIO API and console ports are bound to localhost only; do not expose them publicly unless they are placed behind explicit authentication and access controls.

Use this Caddy block:

```caddyfile
wrnrs.dobrovolskyi.com.ua {
	handle /telegram/* {
		reverse_proxy 127.0.0.1:18087
	}

	handle /healthz {
		reverse_proxy 127.0.0.1:18087
	}

	handle {
		respond "WRNRS bot is running." 200
	}

	encode zstd gzip

	header {
		-Server
		Referrer-Policy "strict-origin-when-cross-origin"
		X-Content-Type-Options "nosniff"
		X-Frame-Options "DENY"
	}
}
```

On this host, editing `/etc/caddy/Caddyfile` requires an interactive sudo password. Run these commands in a shell with sudo access:

```bash
sudo caddy fmt --overwrite /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

## Volumes

- `sqlite-data`: SQLite database.
- `redis-data`: Redis append-only data.
- `minio-data`: uploaded/background assets.

## Backups

Back up SQLite and MinIO together because SQLite stores object keys and MinIO stores object bytes.

Recommended backup unit:

1. Pause bot writes or take a filesystem snapshot.
2. Copy SQLite database plus `-wal`/`-shm` files if present.
3. Copy MinIO bucket data.
4. Resume bot.

## Secrets

Never commit:

- Bot token.
- Phone hash secret.
- Monobank URL/card number.
- MinIO secret.
- Telegram webhook secret.
- Answer encryption key.
- Admin Telegram IDs if they identify private accounts.

Use `.env` or deployment secrets.

## Webhook Setup

Set these values in `.env`:

```env
PUBLIC_BASE_URL=https://wrnrs.dobrovolskyi.com.ua
BOT_USERNAME=<bot-username-without-at>
TELEGRAM_WEBHOOK_SECRET=<generated-hex-secret>
PHONE_HASH_SECRET=<generated-hex-secret>
ANSWER_ENCRYPTION_KEY=<openssl-rand-base64-32>
```

Register the webhook with Telegram using the public URL `https://wrnrs.dobrovolskyi.com.ua/telegram/webhook` and the same secret token. The application rejects webhook requests when the `X-Telegram-Bot-Api-Secret-Token` header does not match.

`BOT_USERNAME` is used to build pairing invite links. `PHONE_HASH_SECRET` is used to HMAC shared contact phone numbers for pairing requests; raw shared partner phone numbers are not stored. `ANSWER_ENCRYPTION_KEY` must be generated with `openssl rand -base64 32` and must remain stable for the life of stored typed answers.

After Caddy is reloaded, register the webhook:

```bash
cd /home/thathunky/bots/wrnrs
set -a
. ./.env
set +a

curl -fsS "https://api.telegram.org/bot${BOT_TOKEN}/setWebhook" \
  -H 'Content-Type: application/json' \
  -d "$(jq -nc \
    --arg url "https://wrnrs.dobrovolskyi.com.ua/telegram/webhook" \
    --arg secret "$TELEGRAM_WEBHOOK_SECRET" \
    '{url:$url, secret_token:$secret, drop_pending_updates:true, allowed_updates:["message","callback_query","inline_query","pre_checkout_query"]}')"
```

## Health

`/healthz` checks SQLite and Redis. MinIO is validated at startup when credentials are configured.
