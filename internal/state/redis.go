package state

import (
	"context"
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
