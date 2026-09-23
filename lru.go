// Package lrucache 提供一个并发安全、支持 TTL 与加权容量的 LRU 缓存。
package lrucache

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// EvictReason 说明一次淘汰回调触发的原因。
type EvictReason int

const (
	// ReasonCapacity 表示因容量（条数或权重总量）超限被淘汰。
	ReasonCapacity EvictReason = iota
	// ReasonExpired 表示因 TTL 过期被移除。
	ReasonExpired
	// ReasonDeleted 表示被 Delete 显式删除。
	ReasonDeleted
	// ReasonReplaced 表示被同键的 Put 覆盖写入。
	ReasonReplaced
)

func (r EvictReason) String() string {
	switch r {
	case ReasonCapacity:
		return "capacity"
	case ReasonExpired:
		return "expired"
	case ReasonDeleted:
		return "deleted"
	case ReasonReplaced:
		return "replaced"
	default:
		return "unknown"
	}
}

// Clock 提供当前时间，注入假时钟即可在测试中控制 TTL。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type entry struct {
	key       string
	value     int
	weight    int64
	expiresAt time.Time // 零值表示永不过期
}

func (e *entry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}

// Item 是 Snapshot 返回的单个键值对。
type Item struct {
	Key   string
	Value int
}

// Cache 是并发安全的 LRU 缓存。所有公开方法都可被多个 goroutine
// 同时调用，整体效果等价于某个串行执行顺序（线性一致）。
type Cache struct {
	mu          sync.Mutex
	capacity    int64 // 权重总量上限；计数模式下等于条数上限
	weightFn    func(key string, value int) int64
	ttl         time.Duration
	clock       Clock
	onEvict     func(key string, value int, reason EvictReason)
	items       map[string]*list.Element
	order       *list.List // 头部最新使用，尾部最久未使用
	count       int
	totalWeight int64
}

// New 创建一个容量为 capacity 的缓存，按条数淘汰。
func New(capacity int) *Cache {
	return newCache(int64(capacity), nil, 0, nil)
}

// NewWithTTL 创建一个容量为 capacity、每条数据存活 ttl 的缓存。
// clock 为 nil 时使用真实时钟。
func NewWithTTL(capacity int, ttl time.Duration, clock Clock) *Cache {
	return newCache(int64(capacity), nil, ttl, clock)
}

// NewWeighted 创建一个按权重总量淘汰的缓存。weight 为 nil 时每条权重为 1。
// 权重取 key 与 value 的函数值，负值按 0 处理。
func NewWeighted(capacity int64, weight func(key string, value int) int64) *Cache {
	return newCache(capacity, weight, 0, nil)
}

func newCache(capacity int64, weightFn func(string, int) int64, ttl time.Duration, clock Clock) *Cache {
	if clock == nil {
		clock = realClock{}
	}
	return &Cache{
		capacity: capacity,
		weightFn: weightFn,
		ttl:      ttl,
		clock:    clock,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// WithOnEvict 注册淘汰回调并返回缓存本身，可链式调用。
// 回调在内部状态更新完成之后、仍持有锁时触发，因此回调观察到的一定是
// 淘汰后的状态，且多次回调的顺序与淘汰顺序一致。回调中不得再调用本缓存的
// 任何方法，否则会死锁。
func (c *Cache) WithOnEvict(fn func(key string, value int, reason EvictReason)) *Cache {
	c.mu.Lock()
	c.onEvict = fn
	c.mu.Unlock()
	return c
}

func (c *Cache) weightOf(key string, value int) int64 {
	if c.weightFn == nil {
		return 1
	}
	if w := c.weightFn(key, value); w > 0 {
		return w
	}
	return 0
}

func (c *Cache) deadline() time.Time {
	if c.ttl <= 0 {
		return time.Time{}
	}
	return c.clock.Now().Add(c.ttl)
}

// Get 读取键对应的值；命中会刷新该键的最近使用位置。
// 过期项按未命中处理并被移除。
func (c *Cache) Get(key string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return 0, false
	}
	e := element.Value.(*entry)
	if e.expired(c.clock.Now()) {
		c.removeElementLocked(element, ReasonExpired)
		return 0, false
	}
	c.order.MoveToFront(element)
	return e.value, true
}

// Put 写入键值；容量满时淘汰最久未使用的键。
// 覆盖已有键会刷新其最近使用位置并重新计算 TTL。
func (c *Cache) Put(key string, value int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	weight := c.weightOf(key, value)
	if element, ok := c.items[key]; ok {
		e := element.Value.(*entry)
		oldValue := e.value
		c.totalWeight += weight - e.weight
		e.value = value
		e.weight = weight
		e.expiresAt = c.deadline()
		c.order.MoveToFront(element)
		c.fireLocked(key, oldValue, ReasonReplaced)
		c.evictToCapacityLocked()
		return
	}
	element := c.order.PushFront(&entry{key: key, value: value, weight: weight, expiresAt: c.deadline()})
	c.items[key] = element
	c.count++
	c.totalWeight += weight
	c.evictToCapacityLocked()
}

// Delete 删除键，返回是否删掉了东西。
func (c *Cache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return false
	}
	c.removeElementLocked(element, ReasonDeleted)
	return true
}

// Len 返回当前键数量（不含已过期项）。
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpiredLocked()
	return c.count
}

// Keys 按从新到旧返回所有未过期的键。
func (c *Cache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpiredLocked()
	keys := make([]string, 0, c.count)
	for element := c.order.Front(); element != nil; element = element.Next() {
		keys = append(keys, element.Value.(*entry).key)
	}
	return keys
}

// Snapshot 一次性返回键与顺序的一致视图：按从新到旧排列的键值对。
// 返回的切片是快照，之后对缓存的修改不影响它。
func (c *Cache) Snapshot() []Item {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpiredLocked()
	items := make([]Item, 0, c.count)
	for element := c.order.Front(); element != nil; element = element.Next() {
		e := element.Value.(*entry)
		items = append(items, Item{Key: e.key, Value: e.value})
	}
	return items
}

// Check 校验内部不变量：链表长度 == map 大小 == 计数，
// 链上权重之和 == totalWeight，且链表与 map 互相指向一致。
func (c *Cache) Check() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.order.Len() != len(c.items) {
		return fmt.Errorf("lrucache: list length %d != map size %d", c.order.Len(), len(c.items))
	}
	if c.count != len(c.items) {
		return fmt.Errorf("lrucache: count %d != map size %d", c.count, len(c.items))
	}
	var weightSum int64
	for element := c.order.Front(); element != nil; element = element.Next() {
		e, ok := element.Value.(*entry)
		if !ok {
			return fmt.Errorf("lrucache: list element has unexpected type %T", element.Value)
		}
		if c.items[e.key] != element {
			return fmt.Errorf("lrucache: map entry for key %q does not point back to its list element", e.key)
		}
		weightSum += e.weight
	}
	if weightSum != c.totalWeight {
		return fmt.Errorf("lrucache: total weight %d != sum of entry weights %d", c.totalWeight, weightSum)
	}
	return nil
}

// evictToCapacityLocked 从尾部淘汰直到权重总量不超上限。
// 调用方必须持有锁。
func (c *Cache) evictToCapacityLocked() {
	for c.totalWeight > c.capacity {
		element := c.order.Back()
		if element == nil {
			return
		}
		c.removeElementLocked(element, ReasonCapacity)
	}
}

// purgeExpiredLocked 移除所有过期项；只删节点不重排，保留其余键的相对顺序。
// 调用方必须持有锁。
func (c *Cache) purgeExpiredLocked() {
	if c.ttl <= 0 {
		return
	}
	now := c.clock.Now()
	for element := c.order.Back(); element != nil; {
		prev := element.Prev()
		if element.Value.(*entry).expired(now) {
			c.removeElementLocked(element, ReasonExpired)
		}
		element = prev
	}
}

// removeElementLocked 从 map 与链表中移除节点并触发回调。
// 调用方必须持有锁。
func (c *Cache) removeElementLocked(element *list.Element, reason EvictReason) {
	e := element.Value.(*entry)
	c.order.Remove(element)
	delete(c.items, e.key)
	c.count--
	c.totalWeight -= e.weight
	c.fireLocked(e.key, e.value, reason)
}

// fireLocked 在状态更新完成后触发回调。调用方必须持有锁。
func (c *Cache) fireLocked(key string, value int, reason EvictReason) {
	if c.onEvict != nil {
		c.onEvict(key, value, reason)
	}
}
