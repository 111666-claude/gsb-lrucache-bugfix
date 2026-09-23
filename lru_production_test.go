package lrucache

import (
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"
)

// --- 缺陷 1：Get 命中必须刷新最近使用顺序 ---

func TestGetRefreshesRecency(t *testing.T) {
	cache := New(2)
	cache.Put("a", 1)
	cache.Put("b", 2)

	if _, ok := cache.Get("a"); !ok {
		t.Fatal("Get(\"a\") should hit")
	}
	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Fatalf("Keys() = %v, want [a b] after Get refreshed a", keys)
	}

	cache.Put("c", 3) // b 才是最久未使用，应淘汰 b 而不是 a
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a was refreshed by Get and must survive eviction")
	}
	if _, ok := cache.Get("b"); ok {
		t.Fatal("b is the least recently used and must be evicted")
	}
}

// --- 缺陷 2：Delete 必须同时删除链表节点 ---

func TestDeleteLeavesNoGhostInKeys(t *testing.T) {
	cache := New(3)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3)

	cache.Delete("b")
	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"c", "a"}) {
		t.Fatalf("Keys() = %v, want [c a] without ghost key", keys)
	}
	if cache.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", cache.Len())
	}
	if err := cache.Check(); err != nil {
		t.Fatalf("Check() after Delete: %v", err)
	}
}

// --- 缺陷 3：容量为 0 时不能留下任何数据 ---

func TestZeroCapacityHoldsNothing(t *testing.T) {
	cache := New(0)
	cache.Put("a", 1)

	if _, ok := cache.Get("a"); ok {
		t.Fatal("zero-capacity cache must not serve any key")
	}
	if cache.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", cache.Len())
	}
	if keys := cache.Keys(); len(keys) != 0 {
		t.Fatalf("Keys() = %v, want empty", keys)
	}
}

// --- TTL ---

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func TestTTLExpiry(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	cache := NewWithTTL(4, 10*time.Second, clock)
	cache.Put("a", 1)
	cache.Put("b", 2)

	clock.Advance(5 * time.Second)
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a should still be alive after 5s of a 10s TTL")
	}

	clock.Advance(6 * time.Second) // a、b 均已过期
	if _, ok := cache.Get("a"); ok {
		t.Fatal("expired a must be reported as a miss")
	}
	if keys := cache.Keys(); len(keys) != 0 {
		t.Fatalf("Keys() = %v, want empty after expiry", keys)
	}
	if cache.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after expiry", cache.Len())
	}
	if err := cache.Check(); err != nil {
		t.Fatalf("Check() after expiry: %v", err)
	}
}

func TestTTLPurgeKeepsRelativeOrder(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	cache := NewWithTTL(10, 10*time.Second, clock)
	cache.Put("a", 1)
	clock.Advance(6 * time.Second) // a 还剩 4s
	cache.Put("b", 2)
	cache.Put("c", 3)

	clock.Advance(5 * time.Second) // a 过期，b、c 仍存活
	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"c", "b"}) {
		t.Fatalf("Keys() = %v, want [c b]: purging must not reorder survivors", keys)
	}
}

func TestOverwriteRefreshesTTL(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	cache := NewWithTTL(2, 10*time.Second, clock)
	cache.Put("a", 1)
	clock.Advance(8 * time.Second)
	cache.Put("a", 2) // 覆盖写入应重新计算 TTL
	clock.Advance(8 * time.Second)

	if value, ok := cache.Get("a"); !ok || value != 2 {
		t.Fatalf("Get(\"a\") = (%d, %v), want (2, true): overwrite must refresh TTL", value, ok)
	}
}

// --- 加权容量 ---

func TestWeightedEvictsByTotalWeight(t *testing.T) {
	cache := NewWeighted(10, func(_ string, value int) int64 { return int64(value) })
	cache.Put("a", 4)
	cache.Put("b", 4)
	cache.Put("c", 4) // 总权重 12 > 10，淘汰最旧的 a

	if _, ok := cache.Get("a"); ok {
		t.Fatal("a (oldest) must be evicted to bring total weight <= 10")
	}
	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"c", "b"}) {
		t.Fatalf("Keys() = %v, want [c b]", keys)
	}
	if err := cache.Check(); err != nil {
		t.Fatalf("Check(): %v", err)
	}
}

func TestWeightedOverwriteAdjustsTotal(t *testing.T) {
	cache := NewWeighted(5, func(_ string, value int) int64 { return int64(value) })
	cache.Put("a", 3)
	cache.Put("b", 2)
	cache.Put("a", 5) // a 变重，总权重 7 > 5，淘汰最旧的 b

	if _, ok := cache.Get("b"); ok {
		t.Fatal("b must be evicted after a grew heavier")
	}
	if value, ok := cache.Get("a"); !ok || value != 5 {
		t.Fatalf("Get(\"a\") = (%d, %v), want (5, true)", value, ok)
	}
}

func TestOverweightItemIsEvictedImmediately(t *testing.T) {
	var evicted []string
	cache := NewWeighted(3, func(_ string, value int) int64 { return int64(value) }).
		WithOnEvict(func(key string, _ int, reason EvictReason) {
			if reason == ReasonCapacity {
				evicted = append(evicted, key)
			}
		})
	cache.Put("big", 10) // 单条权重超过总容量：插入后立即被淘汰

	if _, ok := cache.Get("big"); ok {
		t.Fatal("an item heavier than the whole capacity must not stay in the cache")
	}
	if cache.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", cache.Len())
	}
	if !reflect.DeepEqual(evicted, []string{"big"}) {
		t.Fatalf("evicted = %v, want [big]", evicted)
	}
	if err := cache.Check(); err != nil {
		t.Fatalf("Check(): %v", err)
	}
}

// --- 淘汰回调 ---

type evictEvent struct {
	key    string
	value  int
	reason EvictReason
}

func TestOnEvictReasons(t *testing.T) {
	var mu sync.Mutex
	var events []evictEvent
	record := func(key string, value int, reason EvictReason) {
		mu.Lock()
		events = append(events, evictEvent{key, value, reason})
		mu.Unlock()
	}

	clock := &fakeClock{now: time.Unix(1000, 0)}
	cache := NewWithTTL(2, 10*time.Second, clock).WithOnEvict(record)

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("a", 10) // replaced：旧值 1
	cache.Put("c", 3)  // capacity：淘汰最旧的 b
	clock.Advance(11 * time.Second)
	cache.Get("c") // expired
	cache.Delete("a")

	want := []evictEvent{
		{"a", 1, ReasonReplaced},
		{"b", 2, ReasonCapacity},
		{"c", 3, ReasonExpired},
		{"a", 10, ReasonDeleted},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

func TestOnEvictObservesPostEvictionState(t *testing.T) {
	cache := New(1)
	var lenAtCallback = -1
	cache.WithOnEvict(func(string, int, EvictReason) {
		// 回调在状态更新后触发：此时被淘汰的键必须已经不在缓存里。
		// 注意：这里直接读内部字段，因为回调中不能再加锁调用公开方法。
		lenAtCallback = cache.count
	})
	cache.Put("a", 1)
	cache.Put("b", 2) // 淘汰 a

	if lenAtCallback != 1 {
		t.Fatalf("callback observed count = %d, want 1 (state after eviction)", lenAtCallback)
	}
	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"b"}) {
		t.Fatalf("Keys() = %v, want [b]", keys)
	}
}

// --- Snapshot ---

func TestSnapshotConsistentView(t *testing.T) {
	cache := New(4)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Get("a") // a 变最新

	want := []Item{{Key: "a", Value: 1}, {Key: "b", Value: 2}}
	if got := cache.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot() = %+v, want %+v", got, want)
	}

	snapshot := cache.Snapshot()
	cache.Put("c", 3)
	cache.Delete("a")
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot changed after later mutations: %+v", snapshot)
	}
}

// --- Check 不变量 ---

func TestCheckDetectsCorruption(t *testing.T) {
	cache := New(4)
	cache.Put("a", 1)
	cache.Put("b", 2)
	if err := cache.Check(); err != nil {
		t.Fatalf("Check() on healthy cache: %v", err)
	}

	cache.count++ // 人为破坏不变量
	if err := cache.Check(); err == nil {
		t.Fatal("Check() must report corrupted count")
	}
}

// --- 并发与线性一致 ---

func TestConcurrentStressWithCheck(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	cache := NewWithTTL(16, time.Minute, clock).WithOnEvict(func(string, int, EvictReason) {})

	const workers = 8
	const opsPerWorker = 2000
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h"}

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for i := 0; i < opsPerWorker; i++ {
				key := keys[rng.Intn(len(keys))]
				switch rng.Intn(5) {
				case 0, 1:
					cache.Put(key, rng.Intn(100))
				case 2:
					cache.Delete(key)
				default:
					cache.Get(key)
				}
				if err := cache.Check(); err != nil {
					t.Errorf("Check() failed during stress: %v", err)
					return
				}
			}
		}(int64(worker))
	}
	wg.Wait()

	if err := cache.Check(); err != nil {
		t.Fatalf("Check() after stress: %v", err)
	}
}

func TestConcurrentPutsAreLinearizable(t *testing.T) {
	cache := New(64)
	const workers = 16
	const keysPerWorker = 4

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < keysPerWorker; i++ {
				cache.Put(fmt.Sprintf("w%d-k%d", id, i), id)
			}
		}(worker)
	}
	wg.Wait()

	if got := cache.Len(); got != workers*keysPerWorker {
		t.Fatalf("Len() = %d, want %d: concurrent Puts must all be visible", got, workers*keysPerWorker)
	}
	if err := cache.Check(); err != nil {
		t.Fatalf("Check(): %v", err)
	}
}
