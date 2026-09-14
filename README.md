# ZConfig

轻量级 Go 配置库。配置属性保存在进程内存，真实配置值使用 MongoDB 持久化，并按每项的 `CacheTime` 在本地缓存。

适用于几千项以内的后台配置：分组查询、位标志类型查询、必填项排序、默认值、正则校验，以及 `string`、`int64`、`bool` 类型安全读取。

## 安装

```bash
go get github.com/nanxiangwanwan/ZConfig
```

## MongoDB 数据结构

MongoDB 只保存真实值：

```go
type ConfigValue struct {
    Key       string `bson:"key" json:"key"`
    Value     any    `bson:"value" json:"value"`
    UpdatedAt int64  `bson:"updatedAt" json:"updatedAt"`
}
```

Collection 名称由初始化函数传入。

## 初始化

使用已有的 `*mongo.Database`：

```go
zc, err := zconfig.NewWithDB(db, "config_value")
if err != nil {
    return err
}
```

使用 MongoDB URL：

```go
zc, err := zconfig.NewWithURL(
    "mongodb://127.0.0.1:27017",
    "game",
    "config_value",
)
if err != nil {
    return err
}
defer zc.Close(context.Background())
```

建议初始化时创建 `key` 唯一索引：

```go
if err := zc.EnsureIndexes(context.Background()); err != nil {
    return err
}
```

## 注册配置属性

代码注册：

```go
const (
    TypeSystem int32 = 1 << iota
    TypeUser
    TypePayment
)

err := zc.Register(zconfig.ConfigAttribute{
    Group:        "payment",
    Type:         TypeSystem | TypePayment,
    Title:        "Minimum withdrawal amount",
    Key:          "withdraw_min_amount",
    Explain:      "Minimum amount allowed for a withdrawal",
    VType:        zconfig.ValueTypeNumber,
    DefaultValue: int64(10),
    IsRequired:   true,
    RegExp:       `^[0-9]+$`,
    CacheTime:    60,
})
```

注册切片：

```go
err := zc.Register(attributes...)
```

从 JSON 注册：

```go
err := zc.RegisterJSONFile("config.json")
```

JSON 可以是一个对象或对象数组。`vType` 只能是 `number`、`string`、`boolean`；`number` 在库内统一为 `int64`。

`readOnly: true` 是返回给使用者的属性元数据，后台可据此禁止编辑；ZConfig 的 `Set` 不会强制限制写入，仍可正常保存到 MongoDB。

## 读取和写入

```go
name, err := zc.GetString("site_name")
amount, err := zc.GetInt64("withdraw_min_amount")
enabled, err := zc.GetBool("withdraw_enabled")
value, err := zc.GetAny("custom_key")

err = zc.SetString("site_name", "HAXI")
err = zc.SetInt64("withdraw_min_amount", 100)
err = zc.SetBool("withdraw_enabled", true)

```

读取顺序：

1. 本地缓存存在且未过期，直接返回。
2. 无缓存或已过期，从 MongoDB 读取。
3. MongoDB 没有该 Key，返回 `DefaultValue`。
4. 没有默认值，返回对应零值：`""`、`int64(0)`、`false`。

默认值和零值也会缓存，避免数据库不存在记录时反复查询。

`CacheTime` 单位为秒：

- `0`：首次加载后永不过期。
- `> 0`：超过指定秒数后重新读取 MongoDB。
- `< 0`：不使用本地缓存。

## 批量读取

按指定 Key：

```go
values, err := zc.GetType([]string{"site_name", "withdraw_enabled"})
name, err := values.GetString("site_name")
raw := values.Map()
```

按分组：

```go
payment, err := zc.GetByGroup("payment")
amount, err := payment.GetInt64("withdraw_min_amount")
```

冷缓存时，批量读取会用一次 MongoDB `$in` 查询加载所有缺失 Key，不会逐个 Key 请求数据库。

按位标志类型，命中任意一位即返回：

```go
values, err := zc.GetByType(TypeSystem | TypePayment)
```

## 配置属性和必填排序

属性列表始终在内存中筛选和排序。先加载 MongoDB 中已设置的 Key，再取得列表：

```go
if err := zc.RefreshExistence(context.Background()); err != nil {
    return err
}

attributes := zc.GetConfigAttributes()
```

结果先按 `Group` 分组；同一组内，必填且数据库未设置的配置排在前面，再按 `Title` 和 `Key` 排序。`RefreshExistence` 只读取 Key，不加载真实值。

## 并发安全

`ZConfig` 的注册、读取、写入、缓存失效和查询均可并发调用。同一个 Key 缓存失效时只会有一个请求读取 MongoDB。

配置属性在 2000～3000 项时，内存筛选和排序不会成为性能问题。不过一次把全部属性返回前端通常会产生数百 KB 的 JSON，后台页面建议按 `Group` 请求和展示，以减少网络传输与前端渲染量。

## License

MIT
