package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

var RDB *redis.Client
var RedisEnabled = true

func RedisKeyCacheSeconds() int {
	return SyncFrequency
}

// InitRedisClient This function is called after init()
func InitRedisClient() (err error) {
	if os.Getenv("REDIS_CONN_STRING") == "" {
		RedisEnabled = false
		SysLog("REDIS_CONN_STRING not set, Redis is not enabled")
		return nil
	}
	if os.Getenv("SYNC_FREQUENCY") == "" {
		SysLog("SYNC_FREQUENCY not set, use default value 60")
		SyncFrequency = 60
	}
	SysLog("Redis is enabled")
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	opt.PoolSize = GetEnvOrDefault("REDIS_POOL_SIZE", 10)
	RDB = redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = RDB.Ping(ctx).Result()
	if err != nil {
		FatalLog("Redis ping test failed: " + err.Error())
	}
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis connected to %s", opt.Addr))
		SysLog(fmt.Sprintf("Redis database: %d", opt.DB))
	}
	return err
}

func ParseRedisOption() *redis.Options {
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	return opt
}

var redisIncrWithTTLScript = redis.NewScript(`
local ttl = redis.call("PTTL", KEYS[1])
if ttl > 0 then
	redis.call("INCRBY", KEYS[1], ARGV[1])
	redis.call("PEXPIRE", KEYS[1], ttl)
	return 1
end
return 0
`)

var redisHIncrByWithTTLScript = redis.NewScript(`
local ttl = redis.call("PTTL", KEYS[1])
if ttl > 0 then
	redis.call("HINCRBY", KEYS[1], ARGV[1], ARGV[2])
	redis.call("PEXPIRE", KEYS[1], ttl)
	return 1
end
return 0
`)

const redisCacheVersionSequenceKey = "cache-version:sequence"

var redisGetOrInitCacheVersionScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current then
	return current
end
local next = redis.call("INCR", KEYS[2])
redis.call("SET", KEYS[1], next)
return next
`)

var redisInvalidateVersionedHashScript = redis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local sequence = tonumber(redis.call("GET", KEYS[2]) or "0")
if current > sequence then
	redis.call("SET", KEYS[2], current)
	sequence = current
end
local next = redis.call("INCR", KEYS[2])
redis.call("SET", KEYS[1], next)
redis.call("DEL", KEYS[3])
return next
`)

var redisDeleteVersionedHashScript = redis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local sequence = tonumber(redis.call("GET", KEYS[2]) or "0")
if current > sequence then
	redis.call("SET", KEYS[2], current)
end
redis.call("DEL", KEYS[3])
redis.call("DEL", KEYS[1])
return 1
`)

var errRedisCacheVersionChanged = errors.New("redis cache version changed")

func RedisSet(key string, value string, expiration time.Duration) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis SET: key=%s, value=%s, expiration=%v", key, value, expiration))
	}
	ctx := context.Background()
	return RDB.Set(ctx, key, value, expiration).Err()
}

func RedisGet(key string) (string, error) {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis GET: key=%s", key))
	}
	ctx := context.Background()
	val, err := RDB.Get(ctx, key).Result()
	return val, err
}

//func RedisExpire(key string, expiration time.Duration) error {
//	ctx := context.Background()
//	return RDB.Expire(ctx, key, expiration).Err()
//}
//
//func RedisGetEx(key string, expiration time.Duration) (string, error) {
//	ctx := context.Background()
//	return RDB.GetSet(ctx, key, expiration).Result()
//}

func RedisDel(key string) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis DEL: key=%s", key))
	}
	ctx := context.Background()
	return RDB.Del(ctx, key).Err()
}

func RedisDelKey(key string) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis DEL Key: key=%s", key))
	}
	ctx := context.Background()
	return RDB.Del(ctx, key).Err()
}

func RedisHSetObj(key string, obj interface{}, expiration time.Duration) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HSET: key=%s, obj=%+v, expiration=%v", key, obj, expiration))
	}
	ctx := context.Background()

	data, err := redisHashData(obj)
	if err != nil {
		return err
	}

	txn := RDB.TxPipeline()
	txn.HSet(ctx, key, data)

	// 只有在 expiration 大于 0 时才设置过期时间
	if expiration > 0 {
		txn.Expire(ctx, key, expiration)
	}

	_, err = txn.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}
	return nil
}

func redisHashData(obj interface{}) (map[string]interface{}, error) {
	value := reflect.ValueOf(obj)
	if value.Kind() != reflect.Ptr || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("obj must be a non-nil pointer to a struct, got %T", obj)
	}

	data := make(map[string]interface{})
	v := value.Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Skip DeletedAt field
		if field.Type.String() == "gorm.DeletedAt" {
			continue
		}

		// 处理指针类型
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				data[field.Name] = ""
				continue
			}
			value = value.Elem()
		}

		// 处理布尔类型
		if value.Kind() == reflect.Bool {
			data[field.Name] = strconv.FormatBool(value.Bool())
			continue
		}

		// 其他类型直接转换为字符串
		data[field.Name] = fmt.Sprintf("%v", value.Interface())
	}
	return data, nil
}

// RedisGetCacheVersion reads a version used to fence cache-aside refills.
// A missing key is initialized from one shared monotonic sequence so deleting
// an entity's version marker never reuses its prior generation.
func RedisGetCacheVersion(key string) (int64, error) {
	result, err := redisGetOrInitCacheVersionScript.Run(
		context.Background(), RDB, []string{key, redisCacheVersionSequenceKey},
	).Int64()
	return result, err
}

// RedisHSetObjIfVersion stores a DB snapshot only while its cache generation
// is unchanged. WATCH makes the comparison and write safe across processes.
func RedisHSetObjIfVersion(key, versionKey string, expectedVersion int64, obj interface{}, expiration time.Duration) (bool, error) {
	data, err := redisHashData(obj)
	if err != nil {
		return false, err
	}
	return redisHSetIfVersion(key, versionKey, expectedVersion, data, expiration)
}

// RedisHSetFieldIfVersion is the field-level variant used by narrow cache
// refills that must not overwrite unrelated hash fields.
func RedisHSetFieldIfVersion(key, versionKey string, expectedVersion int64, field string, value interface{}, expiration time.Duration) (bool, error) {
	return redisHSetIfVersion(key, versionKey, expectedVersion, map[string]interface{}{field: value}, expiration)
}

func redisHSetIfVersion(key, versionKey string, expectedVersion int64, data map[string]interface{}, expiration time.Duration) (bool, error) {
	stored := false
	ctx := context.Background()
	err := RDB.Watch(ctx, func(tx *redis.Tx) error {
		version, err := tx.Get(ctx, versionKey).Int64()
		if errors.Is(err, redis.Nil) {
			// A deleted version key must not reset the fence to the initial
			// generation. All live cache readers obtain a non-negative version;
			// treating a missing key as -1 rejects stale in-flight refills.
			version, err = -1, nil
		}
		if err != nil {
			return err
		}
		if version != expectedVersion {
			return errRedisCacheVersionChanged
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, data)
			if expiration > 0 {
				pipe.Expire(ctx, key, expiration)
			}
			return nil
		})
		if err == nil {
			stored = true
		}
		return err
	}, versionKey)
	if errors.Is(err, errRedisCacheVersionChanged) || errors.Is(err, redis.TxFailedErr) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return stored, nil
}

// RedisInvalidateVersionedHash advances the shared generation and removes the
// cached hash atomically, preventing older DB snapshots from refilling it.
func RedisInvalidateVersionedHash(key, versionKey string) error {
	_, err := redisInvalidateVersionedHashScript.Run(
		context.Background(), RDB,
		[]string{versionKey, redisCacheVersionSequenceKey, key},
	).Result()
	return err
}

// RedisDeleteVersionedHash removes an entity cache and its generation marker.
// This is reserved for permanent entity deletion. Cache refills treat a missing
// generation as a fence miss, so deleting the marker cannot re-admit a snapshot
// captured before the entity was removed.
func RedisDeleteVersionedHash(key, versionKey string) error {
	_, err := redisDeleteVersionedHashScript.Run(
		context.Background(), RDB,
		[]string{versionKey, redisCacheVersionSequenceKey, key},
	).Result()
	return err
}

func RedisHGetObj(key string, obj interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HGETALL: key=%s", key))
	}
	ctx := context.Background()

	result, err := RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to load hash from Redis: %w", err)
	}

	if len(result) == 0 {
		return fmt.Errorf("key %s not found in Redis", key)
	}

	// Handle both pointer and non-pointer values
	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Ptr {
		return fmt.Errorf("obj must be a pointer to a struct, got %T", obj)
	}

	v := val.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("obj must be a pointer to a struct, got pointer to %T", v.Interface())
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldName := field.Name
		if value, ok := result[fieldName]; ok {
			fieldValue := v.Field(i)

			// Handle pointer types
			if fieldValue.Kind() == reflect.Ptr {
				if value == "" {
					continue
				}
				if fieldValue.IsNil() {
					fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
				}
				fieldValue = fieldValue.Elem()
			}

			// Enhanced type handling for Token struct
			switch fieldValue.Kind() {
			case reflect.String:
				fieldValue.SetString(value)
			case reflect.Int, reflect.Int64:
				intValue, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse int field %s: %w", fieldName, err)
				}
				fieldValue.SetInt(intValue)
			case reflect.Bool:
				boolValue, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("failed to parse bool field %s: %w", fieldName, err)
				}
				fieldValue.SetBool(boolValue)
			case reflect.Struct:
				// Special handling for gorm.DeletedAt
				if fieldValue.Type().String() == "gorm.DeletedAt" {
					if value != "" {
						timeValue, err := time.Parse(time.RFC3339, value)
						if err != nil {
							return fmt.Errorf("failed to parse DeletedAt field %s: %w", fieldName, err)
						}
						fieldValue.Set(reflect.ValueOf(gorm.DeletedAt{Time: timeValue, Valid: true}))
					}
				}
			default:
				return fmt.Errorf("unsupported field type: %s for field %s", fieldValue.Kind(), fieldName)
			}
		}
	}

	return nil
}

// RedisIncr Add this function to handle atomic increments
func RedisIncr(key string, delta int64) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis INCR: key=%s, delta=%d", key, delta))
	}
	ctx := context.Background()
	if _, err := redisIncrWithTTLScript.Run(ctx, RDB, []string{key}, delta).Result(); err != nil {
		return fmt.Errorf("failed to increment key: %w", err)
	}
	return nil
}

func RedisHIncrBy(key, field string, delta int64) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HINCRBY: key=%s, field=%s, delta=%d", key, field, delta))
	}
	ctx := context.Background()
	if _, err := redisHIncrByWithTTLScript.Run(ctx, RDB, []string{key}, field, delta).Result(); err != nil {
		return fmt.Errorf("failed to increment hash field: %w", err)
	}
	return nil
}

func RedisHSetField(key, field string, value interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HSET field: key=%s, field=%s, value=%v", key, field, value))
	}
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	if ttl > 0 {
		ctx := context.Background()
		txn := RDB.TxPipeline()

		hsetCmd := txn.HSet(ctx, key, field, value)
		if err := hsetCmd.Err(); err != nil {
			return err
		}

		txn.Expire(ctx, key, ttl)

		_, err = txn.Exec(ctx)
		return err
	}
	return nil
}
