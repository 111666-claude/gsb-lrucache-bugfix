# lrucache

一个并发安全的 LRU 缓存包，只用 Go 标准库。支持按条数或按权重淘汰、
TTL 过期、淘汰回调与内部不变量自检。

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
| `New(capacity int) *Cache` | 建一个按条数淘汰、容量为 capacity 的缓存 |
| `NewWithTTL(capacity int, ttl time.Duration, clock Clock) *Cache` | 同上，但每条数据只存活 ttl；clock 为 nil 时用真实时钟 |
| `NewWeighted(capacity int64, weight func(key string, value int) int64) *Cache` | 按权重总量淘汰，weight 为 nil 时每条权重为 1 |
| `(*Cache) WithOnEvict(func(key string, value int, reason EvictReason)) *Cache` | 注册淘汰回调，可链式调用 |
| `(*Cache) Get(key string) (int, bool)` | 读取；命中会刷新该键的最近使用位置；过期按未命中处理 |
| `(*Cache) Put(key string, value int)` | 写入；容量满时淘汰最久未使用的键 |
| `(*Cache) Delete(key string) bool` | 删除；返回是否删掉了东西 |
| `(*Cache) Len() int` | 当前键数量（不含已过期项） |
| `(*Cache) Keys() []string` | 按从新到旧返回所有未过期的键 |
| `(*Cache) Snapshot() []Item` | 一次性返回键与顺序的一致视图（从新到旧的键值对） |
| `(*Cache) Check() error` | 校验内部不变量，一切正常时返回 nil |

## LRU 语义

- 「最近使用」的顺序由**读和写**共同维护：`Get` 命中要刷新，`Put` 覆盖已有键也要刷新。
- 容量满时淘汰顺序最旧的键；`Keys()` 与 `Snapshot()` 的第一项必须是最新使用的键。
- `Delete` 之后，该键不能出现在 `Keys()` 里，也不能再参与淘汰。
- 容量为 0 时任何键都不会留在缓存里：`Put` 进去的数据立即被淘汰。

## 并发与线性一致

- 所有公开方法都可被多个 goroutine 同时调用，内部用一把互斥锁串行化。
- 并发调用的整体效果等价于这些调用按**某个串行顺序**依次执行（线性一致）；
  每个调用都在锁内完成状态更新后才返回。
- `Snapshot()` 在同一把锁内一次取走键与顺序，是某一瞬间的一致视图，
  返回的切片与后续修改互不影响。
- `go test -race ./...` 必须干净。

## TTL 过期

- `NewWithTTL` 的每条数据自写入（或覆盖写入）起存活 ttl；`Put` 覆盖已有键会重新计时。
- 过期是惰性清理：读到过期项按未命中处理并顺手移除；`Keys()`、`Len()`、
  `Snapshot()` 返回前也会清掉所有过期项。
- 清理只删除过期节点、不重排链表，其余键之间的相对顺序保持不变。
- 时钟通过 `Clock` 接口注入，测试里可以用假时钟精确控制时间。

## 加权容量

- `NewWeighted` 按**权重总量**而不是条目数淘汰：每次写入后若总权重超过
  capacity，就从最久未使用的一端开始淘汰，直到不超上限。
- 权重由 `weight(key, value)` 决定；负值按 0 处理；覆盖写入会按新值重新计算权重。
- **单条权重超过总容量**：该条目写入后会立刻（在淘汰完其它更旧的条目之后）
  自己也被淘汰，缓存最终不保留它——容量上限永远不被打破。

## 淘汰回调

- `WithOnEvict` 注册的回调在每次条目离开时触发，`EvictReason` 区分四种原因：
  - `ReasonCapacity`：容量（条数或权重总量）超限被淘汰；
  - `ReasonExpired`：TTL 过期被移除；
  - `ReasonDeleted`：被 `Delete` 显式删除；
  - `ReasonReplaced`：被同键的 `Put` 覆盖（回调拿到的是旧值）。
- **时序保证**：回调在内部状态更新完成之后触发，因此回调观察到的缓存状态
  一定已经反映了这次移除；一次操作引发多次淘汰时，回调按淘汰发生的顺序调用。
- 回调在持锁状态下执行：**回调里不得再调用本缓存的任何方法**，否则死锁；
  需要后处理请把事件记下来、在回调外处理。

## 不变量自检

`Check()` 在持锁状态下校验以下不变量，任一不满足即返回非 nil 错误：

- 链表长度 == map 大小 == 内部计数；
- 链表上各条目权重之和 == 内部权重总量；
- 链表中每个节点的键都能在 map 中指回同一个节点。

并发压力测试（`TestConcurrentStressWithCheck`）让多个 goroutine 随机
Get/Put/Delete，并在每一步之后调用 `Check()`，配合 `-race` 验证这些不变量
在并发下始终成立。

## 测试

```
go test ./...
go vet ./...
go test -race ./...   # 需要 CGO_ENABLED=1 与可用的 C 编译器
```
