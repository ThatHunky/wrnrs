# Development

## Commands

```bash
GOTOOLCHAIN=local go test ./...
gofmt -w cmd internal
```

The module is pinned to Go 1.24-compatible dependencies. Keep `GOTOOLCHAIN=local` in verification to avoid silently raising the project toolchain.

`ANSWER_ENCRYPTION_KEY` is required at runtime and must be a base64-encoded 32-byte key. Use `openssl rand -base64 32` for deployment secrets. Tests that construct `config.Config` directly should use deterministic test bytes only.

## Testing Strategy

The most important rules have focused tests:

- `internal/content`: deck validation, mature filtering, no-repeat cycle selection.
- `internal/game`: invite acceptance, synchronized typed/skip/in-person completion, no-repeat selection, level progression, encrypted reveal behavior, and support prompt cadence.
- `internal/render`: real PNG card output and real WebP upload derivatives.
- `internal/storage`: SQLite migration, answer encryption helper, game session repositories, and core repository behavior.
- `internal/i18n`: brand and text fallback.

Add tests before implementing new behavior.

## Adding Questions

Edit `content/questions.v1.json`.

Rules:

- Every card needs `uk` and `en`.
- Use stable IDs such as `q033`.
- Direct sexual cards must include `tags: ["mature", "sex"]` and `requires_mature_opt_in: true`.
- Do not mark emotionally deep but non-sexual cards mature by default.

## Adding Styles

Edit `content/styles.v1.json`. Premium styles must be unlockable by `premium_access`, purchase receipt, or admin grant.

## Adding Fonts

Edit `content/fonts.v1.json` and place TTF files under `assets/fonts/`. Preserve upstream license files next to the font family. The renderer can accept a selected font path per card while retaining `CARD_FONT_PATH` as the default fallback.

## Uploaded Backgrounds

`render.ProcessUploadedBackground` accepts JPEG, PNG, or WebP, strips metadata by decode/re-encode, center-crops to card aspect ratio, resizes, and encodes WebP.
