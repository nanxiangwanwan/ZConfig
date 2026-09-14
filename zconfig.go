package zconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const defaultOperationTimeout = 5 * time.Second

// ZConfig stores configuration attributes and cached values in memory, while
// persisting only ConfigValue documents in MongoDB.
type ZConfig struct {
	collection  *mongo.Collection
	client      *mongo.Client
	ownedClient bool

	mu         sync.RWMutex
	attributes map[string]ConfigAttribute
	cache      map[string]cacheValue
	known      map[string]bool
	exists     map[string]bool
	keyLocks   sync.Map
	closed     bool
}

// NewWithDB uses an existing MongoDB database connection.
func NewWithDB(db *mongo.Database, collectionName string) (*ZConfig, error) {
	if db == nil {
		return nil, errors.New("mongo database is nil")
	}
	if strings.TrimSpace(collectionName) == "" {
		return nil, errors.New("collection name is empty")
	}
	return newZConfig(db.Collection(collectionName), nil, false), nil
}

// NewWithURL connects to MongoDB using a five-second initialization timeout.
func NewWithURL(mongoURL, databaseName, collectionName string) (*ZConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	defer cancel()
	return NewWithURLContext(ctx, mongoURL, databaseName, collectionName)
}

// NewWithURLContext connects to MongoDB and verifies the connection.
func NewWithURLContext(ctx context.Context, mongoURL, databaseName, collectionName string) (*ZConfig, error) {
	if strings.TrimSpace(mongoURL) == "" {
		return nil, errors.New("mongo url is empty")
	}
	if strings.TrimSpace(databaseName) == "" {
		return nil, errors.New("database name is empty")
	}
	if strings.TrimSpace(collectionName) == "" {
		return nil, errors.New("collection name is empty")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURL))
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongo: %w", err)
	}
	return newZConfig(client.Database(databaseName).Collection(collectionName), client, true), nil
}

func newZConfig(collection *mongo.Collection, client *mongo.Client, owned bool) *ZConfig {
	return &ZConfig{
		collection:  collection,
		client:      client,
		ownedClient: owned,
		attributes:  make(map[string]ConfigAttribute),
		cache:       make(map[string]cacheValue),
		known:       make(map[string]bool),
		exists:      make(map[string]bool),
	}
}

// EnsureIndexes creates the unique MongoDB index required by ZConfig.
func (z *ZConfig) EnsureIndexes(ctx context.Context) error {
	if err := z.ensureOpen(); err != nil {
		return err
	}
	_, err := z.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "key", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("zconfig_key_unique"),
	})
	if err != nil {
		return fmt.Errorf("create key index: %w", err)
	}
	return nil
}

// Close disconnects only clients created by NewWithURL. A database passed to
// NewWithDB remains owned by the caller.
func (z *ZConfig) Close(ctx context.Context) error {
	z.mu.Lock()
	if z.closed {
		z.mu.Unlock()
		return nil
	}
	z.closed = true
	z.mu.Unlock()
	if z.ownedClient && z.client != nil {
		return z.client.Disconnect(ctx)
	}
	return nil
}

// Register atomically registers one or more attributes. Use Register(attrs...)
// for a slice because Go does not support function overloading.
func (z *ZConfig) Register(attributes ...ConfigAttribute) error {
	if err := z.ensureOpen(); err != nil {
		return err
	}
	validated := make([]ConfigAttribute, len(attributes))
	seen := make(map[string]struct{}, len(attributes))
	for i, attribute := range attributes {
		normalized, err := validateAttribute(attribute)
		if err != nil {
			return err
		}
		if _, ok := seen[normalized.Key]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateKey, normalized.Key)
		}
		seen[normalized.Key] = struct{}{}
		validated[i] = normalized
	}

	z.mu.Lock()
	defer z.mu.Unlock()
	if z.closed {
		return ErrClosed
	}
	for _, attribute := range validated {
		if _, ok := z.attributes[attribute.Key]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateKey, attribute.Key)
		}
	}
	for _, attribute := range validated {
		z.attributes[attribute.Key] = attribute
	}
	return nil
}

// RegisterJSON registers either a JSON array or one JSON object.
func (z *ZConfig) RegisterJSON(data []byte) error {
	var attributes []ConfigAttribute
	if err := json.Unmarshal(data, &attributes); err == nil {
		return z.Register(attributes...)
	}
	var attribute ConfigAttribute
	if err := json.Unmarshal(data, &attribute); err != nil {
		return fmt.Errorf("decode config attributes: %w", err)
	}
	return z.Register(attribute)
}

func (z *ZConfig) RegisterJSONFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config attributes: %w", err)
	}
	return z.RegisterJSON(data)
}

func (z *ZConfig) GetAny(key string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	defer cancel()
	return z.GetAnyContext(ctx, key)
}

func (z *ZConfig) GetAnyContext(ctx context.Context, key string) (any, error) {
	attribute, err := z.attribute(key)
	if err != nil {
		return nil, err
	}
	if value, ok := z.cached(attribute); ok {
		return value, nil
	}

	lock := z.keyLock(key)
	lock.Lock()
	defer lock.Unlock()
	if value, ok := z.cached(attribute); ok {
		return value, nil
	}

	var document ConfigValue
	err = z.collection.FindOne(ctx, bson.M{"key": key}).Decode(&document)
	exists := true
	if errors.Is(err, mongo.ErrNoDocuments) {
		exists = false
		document.Value = defaultOrZero(attribute)
	} else if err != nil {
		return nil, fmt.Errorf("load config %q: %w", key, err)
	}
	value, err := normalizeValue(attribute.VType, document.Value)
	if err != nil {
		return nil, fmt.Errorf("config %q: %w", key, err)
	}
	z.mu.Lock()
	z.known[key], z.exists[key] = true, exists
	if attribute.CacheTime >= 0 {
		z.cache[key] = cacheValue{Value: value, LoadedAt: time.Now(), Exists: exists}
	} else {
		delete(z.cache, key)
	}
	z.mu.Unlock()
	return value, nil
}

func (z *ZConfig) GetString(key string) (string, error) {
	value, err := z.GetAny(key)
	if err != nil {
		return "", err
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%w: %s is not string", ErrValueTypeMismatch, key)
	}
	return result, nil
}

func (z *ZConfig) GetInt64(key string) (int64, error) {
	value, err := z.GetAny(key)
	if err != nil {
		return 0, err
	}
	result, ok := toInt64(value)
	if !ok {
		return 0, fmt.Errorf("%w: %s is not number", ErrValueTypeMismatch, key)
	}
	return result, nil
}

func (z *ZConfig) GetBool(key string) (bool, error) {
	value, err := z.GetAny(key)
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s is not boolean", ErrValueTypeMismatch, key)
	}
	return result, nil
}

// GetType returns the values of the explicitly supplied keys.
func (z *ZConfig) GetType(keys []string) (*Values, error) {
	return z.getKeys(context.Background(), keys)
}

// GetByType returns attributes matching any bit in mask (attribute.Type&mask != 0).
func (z *ZConfig) GetByType(mask int32) (*Values, error) {
	z.mu.RLock()
	keys := make([]string, 0)
	for key, attribute := range z.attributes {
		if attribute.Type&mask != 0 {
			keys = append(keys, key)
		}
	}
	z.mu.RUnlock()
	sort.Strings(keys)
	return z.getKeys(context.Background(), keys)
}

func (z *ZConfig) GetByGroup(group string) (*Values, error) {
	z.mu.RLock()
	keys := make([]string, 0)
	for key, attribute := range z.attributes {
		if attribute.Group == group {
			keys = append(keys, key)
		}
	}
	z.mu.RUnlock()
	sort.Strings(keys)
	return z.getKeys(context.Background(), keys)
}

func (z *ZConfig) getKeys(parent context.Context, keys []string) (*Values, error) {
	ctx, cancel := context.WithTimeout(parent, defaultOperationTimeout)
	defer cancel()
	result := make(map[string]any, len(keys))
	attributes := make(map[string]ConfigAttribute, len(keys))
	missingKeys := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))

	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		attribute, err := z.attribute(key)
		if err != nil {
			return nil, err
		}
		attributes[key] = attribute
		if value, ok := z.cached(attribute); ok {
			result[key] = value
			continue
		}
		missingKeys = append(missingKeys, key)
	}
	if len(missingKeys) == 0 {
		return newValues(result), nil
	}

	cursor, err := z.collection.Find(ctx, bson.M{"key": bson.M{"$in": missingKeys}})
	if err != nil {
		return nil, fmt.Errorf("load config values: %w", err)
	}
	defer cursor.Close(ctx)
	found := make(map[string]any, len(missingKeys))
	for cursor.Next(ctx) {
		var document ConfigValue
		if err := cursor.Decode(&document); err != nil {
			return nil, fmt.Errorf("decode config value: %w", err)
		}
		found[document.Key] = document.Value
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate config values: %w", err)
	}

	loadedAt := time.Now()
	z.mu.Lock()
	defer z.mu.Unlock()
	for _, key := range missingKeys {
		attribute := attributes[key]
		raw, exists := found[key]
		if !exists {
			raw = defaultOrZero(attribute)
		}
		value, err := normalizeValue(attribute.VType, raw)
		if err != nil {
			return nil, fmt.Errorf("config %q: %w", key, err)
		}
		result[key] = value
		z.known[key], z.exists[key] = true, exists
		if attribute.CacheTime >= 0 {
			z.cache[key] = cacheValue{Value: value, LoadedAt: loadedAt, Exists: exists}
		}
	}
	return newValues(result), nil
}

func (z *ZConfig) Set(key string, value any) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	defer cancel()
	return z.SetContext(ctx, key, value)
}

func (z *ZConfig) SetString(key, value string) error { return z.Set(key, value) }
func (z *ZConfig) SetInt64(key string, value int64) error { return z.Set(key, value) }
func (z *ZConfig) SetBool(key string, value bool) error { return z.Set(key, value) }

func (z *ZConfig) SetContext(ctx context.Context, key string, value any) error {
	attribute, err := z.attribute(key)
	if err != nil {
		return err
	}
	normalized, err := validateValue(attribute, value)
	if err != nil {
		return fmt.Errorf("config %q: %w", key, err)
	}
	updatedAt := time.Now().Unix()
	_, err = z.collection.UpdateOne(
		ctx,
		bson.M{"key": key},
		bson.M{"$set": bson.M{"value": normalized, "updatedAt": updatedAt}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("save config %q: %w", key, err)
	}
	z.mu.Lock()
	z.known[key], z.exists[key] = true, true
	if attribute.CacheTime >= 0 {
		z.cache[key] = cacheValue{Value: normalized, LoadedAt: time.Now(), Exists: true}
	} else {
		delete(z.cache, key)
	}
	z.mu.Unlock()
	return nil
}

// Invalidate removes one local cached value. The next read reloads MongoDB.
func (z *ZConfig) Invalidate(key string) {
	z.mu.Lock()
	delete(z.cache, key)
	delete(z.known, key)
	delete(z.exists, key)
	z.mu.Unlock()
}

func (z *ZConfig) InvalidateAll() {
	z.mu.Lock()
	z.cache = make(map[string]cacheValue)
	z.known = make(map[string]bool)
	z.exists = make(map[string]bool)
	z.mu.Unlock()
}

// RefreshExistence loads only MongoDB keys, not their values. Call it before
// GetConfigAttributes when the required-but-unset ordering must be exact.
func (z *ZConfig) RefreshExistence(ctx context.Context) error {
	if err := z.ensureOpen(); err != nil {
		return err
	}
	z.mu.RLock()
	keys := make([]string, 0, len(z.attributes))
	for key := range z.attributes {
		keys = append(keys, key)
	}
	z.mu.RUnlock()
	if len(keys) == 0 {
		return nil
	}
	cursor, err := z.collection.Find(ctx, bson.M{"key": bson.M{"$in": keys}}, options.Find().SetProjection(bson.M{"key": 1, "_id": 0}))
	if err != nil {
		return fmt.Errorf("load config keys: %w", err)
	}
	defer cursor.Close(ctx)
	found := make(map[string]bool, len(keys))
	for cursor.Next(ctx) {
		var item struct {
			Key string `bson:"key"`
		}
		if err := cursor.Decode(&item); err != nil {
			return fmt.Errorf("decode config key: %w", err)
		}
		found[item.Key] = true
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate config keys: %w", err)
	}
	z.mu.Lock()
	for _, key := range keys {
		z.known[key] = true
		z.exists[key] = found[key]
	}
	z.mu.Unlock()
	return nil
}

// GetConfigAttributes returns a copy grouped by Group. Inside every group,
// required values known to be absent in MongoDB come first, followed by title
// and key. Call RefreshExistence first for exact missing-value information.
func (z *ZConfig) GetConfigAttributes() []ConfigAttribute {
	z.mu.RLock()
	result := make([]ConfigAttribute, 0, len(z.attributes))
	missing := make(map[string]bool, len(z.attributes))
	for key, attribute := range z.attributes {
		result = append(result, attribute)
		missing[key] = attribute.IsRequired && z.known[key] && !z.exists[key]
	}
	z.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].Group != result[j].Group {
			return result[i].Group < result[j].Group
		}
		if missing[result[i].Key] != missing[result[j].Key] {
			return missing[result[i].Key]
		}
		if result[i].Title != result[j].Title {
			return result[i].Title < result[j].Title
		}
		return result[i].Key < result[j].Key
	})
	return result
}

func (z *ZConfig) attribute(key string) (ConfigAttribute, error) {
	if err := z.ensureOpen(); err != nil {
		return ConfigAttribute{}, err
	}
	z.mu.RLock()
	attribute, ok := z.attributes[key]
	z.mu.RUnlock()
	if !ok {
		return ConfigAttribute{}, fmt.Errorf("%w: %s", ErrKeyNotRegistered, key)
	}
	return attribute, nil
}

func (z *ZConfig) cached(attribute ConfigAttribute) (any, bool) {
	if attribute.CacheTime < 0 {
		return nil, false
	}
	z.mu.RLock()
	item, ok := z.cache[attribute.Key]
	z.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if attribute.CacheTime == 0 || time.Since(item.LoadedAt) < time.Duration(attribute.CacheTime)*time.Second {
		return item.Value, true
	}
	return nil, false
}

func (z *ZConfig) ensureOpen() error {
	z.mu.RLock()
	closed := z.closed
	z.mu.RUnlock()
	if closed {
		return ErrClosed
	}
	return nil
}

func (z *ZConfig) keyLock(key string) *sync.Mutex {
	lock, _ := z.keyLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func validateAttribute(attribute ConfigAttribute) (ConfigAttribute, error) {
	attribute.Key = strings.TrimSpace(attribute.Key)
	attribute.Group = strings.TrimSpace(attribute.Group)
	if attribute.Key == "" {
		return ConfigAttribute{}, ErrEmptyKey
	}
	if attribute.VType != ValueTypeString && attribute.VType != ValueTypeNumber && attribute.VType != ValueTypeBoolean {
		return ConfigAttribute{}, fmt.Errorf("%w: %s", ErrInvalidValueType, attribute.VType)
	}
	if attribute.RegExp != "" {
		if _, err := regexp.Compile(attribute.RegExp); err != nil {
			return ConfigAttribute{}, fmt.Errorf("invalid regexp for %q: %w", attribute.Key, err)
		}
	}
	if attribute.DefaultValue != nil {
		normalized, err := validateValue(attribute, attribute.DefaultValue)
		if err != nil {
			return ConfigAttribute{}, fmt.Errorf("invalid default for %q: %w", attribute.Key, err)
		}
		attribute.DefaultValue = normalized
	}
	return attribute, nil
}

func validateValue(attribute ConfigAttribute, value any) (any, error) {
	if value == nil {
		if attribute.IsRequired {
			return nil, ErrRequired
		}
		return defaultOrZero(attribute), nil
	}
	normalized, err := normalizeValue(attribute.VType, value)
	if err != nil {
		return nil, err
	}
	if attribute.IsRequired {
		if text, ok := normalized.(string); ok && strings.TrimSpace(text) == "" {
			return nil, ErrRequired
		}
	}
	if attribute.RegExp != "" {
		matched, err := regexp.MatchString(attribute.RegExp, valueString(normalized))
		if err != nil {
			return nil, err
		}
		if !matched {
			return nil, ErrRegExpMismatch
		}
	}
	return normalized, nil
}

func normalizeValue(valueType ValueType, value any) (any, error) {
	switch valueType {
	case ValueTypeString:
		result, ok := value.(string)
		if !ok {
			return nil, ErrValueTypeMismatch
		}
		return result, nil
	case ValueTypeBoolean:
		result, ok := value.(bool)
		if !ok {
			return nil, ErrValueTypeMismatch
		}
		return result, nil
	case ValueTypeNumber:
		result, ok := toInt64(value)
		if !ok {
			return nil, ErrValueTypeMismatch
		}
		return result, nil
	default:
		return nil, ErrInvalidValueType
	}
}

func toInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int8:
		return int64(number), true
	case int16:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case uint:
		if uint64(number) <= math.MaxInt64 {
			return int64(number), true
		}
	case uint8:
		return int64(number), true
	case uint16:
		return int64(number), true
	case uint32:
		return int64(number), true
	case uint64:
		if number <= math.MaxInt64 {
			return int64(number), true
		}
	case float32:
		value64 := float64(number)
		if value64 == math.Trunc(value64) && value64 >= math.MinInt64 && value64 <= math.MaxInt64 {
			return int64(value64), true
		}
	case float64:
		if number == math.Trunc(number) && number >= math.MinInt64 && number <= math.MaxInt64 {
			return int64(number), true
		}
	case json.Number:
		result, err := number.Int64()
		return result, err == nil
	}
	return 0, false
}

func defaultOrZero(attribute ConfigAttribute) any {
	if attribute.DefaultValue != nil {
		return attribute.DefaultValue
	}
	switch attribute.VType {
	case ValueTypeString:
		return ""
	case ValueTypeNumber:
		return int64(0)
	case ValueTypeBoolean:
		return false
	default:
		return nil
	}
}

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}
