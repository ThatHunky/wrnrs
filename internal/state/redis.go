package state

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"wrnrs/internal/game"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr, password string) *RedisStore {
	return &RedisStore{client: redis.NewClient(&redis.Options{Addr: addr, Password: password})}
}

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}

func (s *RedisStore) SetFSM(ctx context.Context, userID int64, value string, ttl time.Duration) error {
	return s.client.Set(ctx, fsmKey(userID), value, ttl).Err()
}

func (s *RedisStore) GetFSM(ctx context.Context, userID int64) (string, error) {
	value, err := s.client.Get(ctx, fsmKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return value, err
}

func (s *RedisStore) ClearFSM(ctx context.Context, userID int64) error {
	return s.client.Del(ctx, fsmKey(userID)).Err()
}

func (s *RedisStore) SetPendingGameCompletion(ctx context.Context, userID int64, completion game.Completion, ttl time.Duration) error {
	data, err := json.Marshal(completion)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, pendingGameCompletionKey(userID), data, ttl).Err()
}

func (s *RedisStore) PendingGameCompletion(ctx context.Context, userID int64) (game.Completion, bool, error) {
	value, err := s.client.Get(ctx, pendingGameCompletionKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return game.Completion{}, false, nil
	}
	if err != nil {
		return game.Completion{}, false, err
	}
	var completion game.Completion
	if err := json.Unmarshal([]byte(value), &completion); err != nil {
		return game.Completion{}, false, err
	}
	return completion, true, nil
}

func (s *RedisStore) ClearPendingGameCompletion(ctx context.Context, userID int64) error {
	return s.client.Del(ctx, pendingGameCompletionKey(userID)).Err()
}

func (s *RedisStore) AllowUserAction(ctx context.Context, userID int64, action string, limit int, window time.Duration) (bool, error) {
	if limit <= 0 || window <= 0 {
		return true, nil
	}
	key := rateLimitKey(userID, action)
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := s.client.Expire(ctx, key, window).Err(); err != nil {
			return false, err
		}
	}
	return count <= int64(limit), nil
}

func (s *RedisStore) WithPairLock(ctx context.Context, pairID int64, ttl time.Duration, fn func() error) error {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	key := pairLockKey(pairID)
	token, err := lockToken()
	if err != nil {
		return err
	}
	locked, err := s.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("pair lock is busy")
	}
	defer func() {
		_ = s.client.Eval(ctx, `
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("DEL", KEYS[1])
			end
			return 0
		`, []string{key}, token).Err()
	}()
	return fn()
}

func (s *RedisStore) CacheFileID(ctx context.Context, renderHash, fileID string, ttl time.Duration) error {
	return s.client.Set(ctx, "render:file:"+renderHash, fileID, ttl).Err()
}

func (s *RedisStore) FileID(ctx context.Context, renderHash string) (string, error) {
	value, err := s.client.Get(ctx, "render:file:"+renderHash).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return value, err
}

func fsmKey(userID int64) string {
	return "fsm:user:" + strconv.FormatInt(userID, 10)
}

func pendingGameCompletionKey(userID int64) string {
	return "game:completion:user:" + strconv.FormatInt(userID, 10)
}

func rateLimitKey(userID int64, action string) string {
	return "rate:user:" + strconv.FormatInt(userID, 10) + ":" + action
}

func pairLockKey(pairID int64) string {
	return "lock:pair:" + strconv.FormatInt(pairID, 10)
}

func lockToken() (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
