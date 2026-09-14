package zconfig

import "time"

// ValueType is the data type of a configuration value.
type ValueType string

const (
	ValueTypeNumber  ValueType = "number"
	ValueTypeString  ValueType = "string"
	ValueTypeBoolean ValueType = "boolean"
)

// ConfigAttribute describes a configuration item. Attributes are held in
// memory and can be registered from Go code or JSON.
type ConfigAttribute struct {
	Group        string    `bson:"group" json:"group"`
	Type         int32     `bson:"type" json:"type"`
	Title        string    `bson:"title" json:"title"`
	Key          string    `bson:"key" json:"key"`
	Explain      string    `bson:"explain" json:"explain"`
	VType        ValueType `bson:"vType" json:"vType"`
	DefaultValue any       `bson:"defaultValue" json:"defaultValue"`
	IsRequired   bool      `bson:"isRequired" json:"isRequired"`
	// ReadOnly prevents writes made through SetFromAdmin. Code may still use
	// Set to persist a value or SetLocal to keep a process-local override.
	ReadOnly     bool      `bson:"readOnly" json:"readOnly"`
	RegExp       string    `bson:"regExp" json:"regExp"`
	// CacheTime is the local cache lifetime in seconds. Zero means that the
	// cached value never expires. A negative value disables local caching.
	CacheTime int32 `bson:"cacheTime" json:"cacheTime"`
}

// ConfigValue is the only document shape persisted in MongoDB.
type ConfigValue struct {
	Key       string `bson:"key" json:"key"`
	Value     any    `bson:"value" json:"value"`
	UpdatedAt int64  `bson:"updatedAt" json:"updatedAt"`
}

type cacheValue struct {
	Value    any
	LoadedAt time.Time
	Exists   bool
}
