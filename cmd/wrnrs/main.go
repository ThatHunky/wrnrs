package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"wrnrs/internal/app"
	"wrnrs/internal/catalog"
	"wrnrs/internal/config"
	"wrnrs/internal/content"
	"wrnrs/internal/i18n"
	"wrnrs/internal/modules"
	"wrnrs/internal/objectstore"
	"wrnrs/internal/positions"
	"wrnrs/internal/render"
	"wrnrs/internal/state"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.BotToken == "" {
		return errors.New("BOT_TOKEN is required")
	}

	db, err := storage.OpenSQLite(ctx, cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer db.Close()
	repo := storage.NewRepository(db)

	redisStore := state.NewRedisStore(cfg.RedisAddr, cfg.RedisPassword)
	defer redisStore.Close()
	if err := retryUntilSuccess(ctx, retryOptions{Attempts: 30, Delay: time.Second, Sleep: sleepContext}, func() error {
		return redisStore.Ping(ctx)
	}); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}

	var minioStore *objectstore.MinIOStore
	var positionsStore *objectstore.MinIOStore
	if cfg.MinIO.AccessKey != "" || cfg.MinIO.SecretKey != "" {
		var err error
		minioStore, err = objectstore.NewMinIOStore(objectstore.MinIOConfig{
			Endpoint:  cfg.MinIO.Endpoint,
			AccessKey: cfg.MinIO.AccessKey,
			SecretKey: cfg.MinIO.SecretKey,
			Bucket:    cfg.MinIO.Bucket,
			UseSSL:    cfg.MinIO.UseSSL,
		})
		if err != nil {
			return err
		}
		if err := retryUntilSuccess(ctx, retryOptions{Attempts: 30, Delay: time.Second, Sleep: sleepContext}, func() error {
			return minioStore.EnsureBucket(ctx)
		}); err != nil {
			return err
		}

		// The positions catalog's images live under POSITIONS_BUCKET, which
		// only diverges from MINIO_BUCKET when an operator explicitly
		// configures it that way — see docs/ARCHITECTURE.md's "source-
		// swappable by config" claim. When they match (the common case, and
		// both default to the same value) reuse minioStore instead of
		// opening a second client for the same bucket.
		if cfg.PositionsBucket == cfg.MinIO.Bucket {
			positionsStore = minioStore
		} else {
			positionsStore, err = objectstore.NewMinIOStore(objectstore.MinIOConfig{
				Endpoint:  cfg.MinIO.Endpoint,
				AccessKey: cfg.MinIO.AccessKey,
				SecretKey: cfg.MinIO.SecretKey,
				Bucket:    cfg.PositionsBucket,
				UseSSL:    cfg.MinIO.UseSSL,
			})
			if err != nil {
				return err
			}
			if err := retryUntilSuccess(ctx, retryOptions{Attempts: 30, Delay: time.Second, Sleep: sleepContext}, func() error {
				return positionsStore.EnsureBucket(ctx)
			}); err != nil {
				return err
			}
		}
	}

	bundle, err := loadI18N("content/i18n")
	if err != nil {
		return err
	}
	deck, err := loadDeck("content/questions.v1.json")
	if err != nil {
		return err
	}
	fonts, err := loadFontCatalog("content/fonts.v1.json", ".")
	if err != nil {
		return err
	}
	styles, err := loadStyleCatalog("content/styles.v1.json")
	if err != nil {
		return err
	}
	backgrounds, err := loadBackgroundCatalog("content/backgrounds.v1.json")
	if err != nil {
		return err
	}

	// Ensure built-in backgrounds are generated
	if err := render.EnsureBuiltInBackgrounds("assets/backgrounds"); err != nil {
		return fmt.Errorf("ensure built-in backgrounds: %w", err)
	}

	bot := telegram.NewClient(cfg.BotToken)
	application := app.New(app.Options{
		Config:      cfg,
		Bot:         bot,
		Repo:        repo,
		State:       redisStore,
		I18N:        bundle,
		Deck:        deck,
		Renderer:    render.NewCardRenderer(render.CardRendererOptions{FontPath: cfg.CardFontPath}),
		Styles:      styles,
		Backgrounds: backgrounds,
		Fonts:       fonts,
		ObjectStore: appObjectStore(minioStore),
		Logger:      logger,
	})

	// The positions catalog module is optional content, not core plumbing:
	// a missing or invalid content/positions.v1.json must never stop the
	// bot — it must only leave that one module unregistered, exactly like
	// MinIO being unconfigured above degrades uploads instead of failing
	// boot. Register() failing is different: an empty id or a colliding
	// callback prefix is a programming mistake in this wiring, not a
	// runtime content problem, so it is returned and fails startup loudly.
	positionsCatalog, err := loadPositionsCatalog(cfg.PositionsCatalogPath)
	if err != nil {
		logger.Warn("positions catalog unavailable; module disabled", "err", err)
	} else if err := positionsCatalog.Validate([]string{"uk", "en"}); err != nil {
		logger.Warn("positions catalog invalid; module disabled", "err", err)
	} else {
		positionsHandler := positions.NewHandler(positions.HandlerOptions{
			Service:     positions.NewService(positions.ServiceOptions{Catalog: positionsCatalog}),
			Catalog:     positionsCatalog,
			Repository:  repo,
			Bot:         bot,
			State:       redisStore,
			ObjectStore: positionsObjectStore(positionsStore),
			I18n:        bundle,
		})
		if err := application.Registry().Register(modules.Module{
			ID:             "positions",
			TitleKey:       "module.positions",
			Icon:           "🎲",
			CallbackPrefix: "pos:",
			Gate:           modules.Gate{Needs18Plus: true, NeedsMature: true},
			Handler:        positionsHandler,
		}); err != nil {
			return fmt.Errorf("register positions module: %w", err)
		}
	}

	server := &http.Server{
		Addr:              ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		Handler:           routes(application, db, redisStore, logger, cfg.TelegramWebhookSecret),
	}
	go func() {
		logger.Info("http server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			stop()
		}
	}()

	if cfg.PublicBaseURL == "" {
		go poll(ctx, bot, application, logger)
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func routes(application *app.App, db *sql.DB, redisStore *state.RedisStore, logger *slog.Logger, webhookSecret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "sqlite unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := redisStore.Ping(ctx); err != nil {
			http.Error(w, "redis unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/telegram/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !webhookSecretMatches(webhookSecret, r.Header.Get("X-Telegram-Bot-Api-Secret-Token")) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var update telegram.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "bad update", http.StatusBadRequest)
			return
		}
		if err := application.HandleUpdate(r.Context(), update); err != nil {
			logger.Error("handle webhook update failed", "error", err, "update_id", update.UpdateID)
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// appObjectStore and positionsObjectStore box *objectstore.MinIOStore into
// their respective narrow interfaces, but only when store is genuinely
// non-nil. minioStore/positionsStore are typed *objectstore.MinIOStore and
// stay nil when MinIO is unconfigured; assigning a nil pointer of that
// concrete type directly to an interface-typed field (app.Options.
// ObjectStore, positions.HandlerOptions.ObjectStore) would box a typed nil
// into a non-nil interface value. Every `!= nil` guard downstream
// (app.go's and handler.go's "ObjectStore can be absent" checks) would
// then incorrectly report the store as present, and the first method call
// would panic dereferencing a nil receiver — with no recover() anywhere in
// this codebase, that panic takes the whole process down. Routing every
// assignment through these functions is what keeps the interface field
// itself nil, not just the pointer inside it, whenever the store is unset.
func appObjectStore(store *objectstore.MinIOStore) app.ObjectStore {
	if store == nil {
		return nil
	}
	return store
}

func positionsObjectStore(store *objectstore.MinIOStore) positions.ObjectStore {
	if store == nil {
		return nil
	}
	return store
}

func webhookSecretMatches(configured, received string) bool {
	if configured == "" {
		return true
	}
	if received == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(configured), []byte(received)) == 1
}

type retryOptions struct {
	Attempts int
	Delay    time.Duration
	Sleep    func(context.Context, time.Duration) error
}

func retryUntilSuccess(ctx context.Context, options retryOptions, fn func() error) error {
	attempts := options.Attempts
	if attempts <= 0 {
		attempts = 1
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if attempt == attempts {
				break
			}
			if sleepErr := sleep(ctx, options.Delay); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		return nil
	}
	return lastErr
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func poll(ctx context.Context, bot *telegram.Client, application *app.App, logger *slog.Logger) {
	var offset int64
	for ctx.Err() == nil {
		updates, err := bot.GetUpdates(ctx, offset)
		if err != nil {
			logger.Error("poll getUpdates failed", "error", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if err := application.HandleUpdate(ctx, update); err != nil {
				logger.Error("handle polled update failed", "error", err, "update_id", update.UpdateID)
			}
		}
	}
}

func loadDeck(path string) (*content.Deck, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	deck, err := content.LoadDeck(file)
	if err != nil {
		return nil, err
	}
	if err := deck.Validate([]string{"uk", "en"}); err != nil {
		return nil, err
	}
	return deck, nil
}

func loadI18N(dir string) (*i18n.Bundle, error) {
	bundle := i18n.NewBundle()
	for _, lang := range []string{"uk", "en"} {
		path := filepath.Join(dir, lang+".json")
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		catalog, err := i18n.LoadCatalog(file)
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		bundle.Add(catalog)
	}
	return bundle, nil
}

func loadFontCatalog(path, root string) (*content.FontCatalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	catalog, err := content.LoadFontCatalog(file)
	if err != nil {
		return nil, err
	}
	if err := catalog.Validate(root); err != nil {
		return nil, err
	}
	return catalog, nil
}

func loadStyleCatalog(path string) (*content.StyleCatalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	catalog, err := content.LoadStyleCatalog(file)
	if err != nil {
		return nil, err
	}
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	return catalog, nil
}

// loadPositionsCatalog only opens and decodes the file; it deliberately does
// not call Validate itself (unlike loadDeck/loadStyleCatalog/
// loadBackgroundCatalog above) so its caller can log a warning and disable
// the module on a validation failure instead of failing boot the way a bad
// deck or style catalog does.
func loadPositionsCatalog(path string) (*catalog.Catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return catalog.Load(file)
}

func loadBackgroundCatalog(path string) (*content.BackgroundCatalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	catalog, err := content.LoadBackgroundCatalog(file)
	if err != nil {
		return nil, err
	}
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	return catalog, nil
}
