# lrucache

一个固定容量的 LRU 缓存包，只用 Go 标准库。

```go
c := lrucache.New(2)
c.Put("a", 1)
c.Put("b", 2)
c.Get("a")      // 命中，a 变成最近使用
c.Put("c", 3)   // 淘汰最久未使用的 b
```

## 公开 API

| 方法 | 说明 |
|---|---|
| `New(capacity int) *Cache` | 建一个容量为 capacity 的缓存 |
| `(*Cache) Get(key string) (int, bool)` | 读取；命中会刷新该键的最近使用位置 |
| `(*Cache) Put(key string, value int)` | 写入；容量满时淘汰最久未使用的键 |
| `(*Cache) Delete(key string) bool` | 删除；返回是否删掉了东西 |
| `(*Cache) Len() int` | 当前键数量 |
| `(*Cache) Keys() []string` | 按从新到旧返回所有键 |

## LRU 语义

- 「最近使用」的顺序由**读和写**共同维护：`Get` 命中要刷新，`Put` 覆盖已有键也要刷新。
- 容量满时淘汰顺序最旧的键；`Keys()` 的第一项必须是最新使用的键。
- `Delete` 之后，该键不能出现在 `Keys()` 里，也不能再参与淘汰。
- 容量为 0 时任何键都不应该留在缓存里。

## 测试

```
go test ./...
```
