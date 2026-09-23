package lrucache

import (
	"reflect"
	"testing"
)

func TestPutAndGet(t *testing.T) {
	cache := New(2)
	cache.Put("a", 1)

	value, ok := cache.Get("a")
	if !ok || value != 1 {
		t.Fatalf("Get(\"a\") = (%d, %v), want (1, true)", value, ok)
	}
}

func TestGetMissing(t *testing.T) {
	cache := New(2)
	if _, ok := cache.Get("nope"); ok {
		t.Fatal("Get on missing key should return false")
	}
}

func TestOverwriteValue(t *testing.T) {
	cache := New(2)
	cache.Put("a", 1)
	cache.Put("a", 2)

	value, ok := cache.Get("a")
	if !ok || value != 2 {
		t.Fatalf("Get(\"a\") = (%d, %v), want (2, true)", value, ok)
	}
	if cache.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", cache.Len())
	}
}

func TestEvictsWhenFull(t *testing.T) {
	cache := New(2)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3)

	if cache.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", cache.Len())
	}
	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"c", "b"}) {
		t.Fatalf("Keys() = %v, want [c b]", keys)
	}
	if _, ok := cache.Get("c"); !ok {
		t.Fatal("the newest key must stay")
	}
}

func TestDeleteRemovesKey(t *testing.T) {
	cache := New(2)
	cache.Put("a", 1)

	if !cache.Delete("a") {
		t.Fatal("Delete should report true for an existing key")
	}
	if _, ok := cache.Get("a"); ok {
		t.Fatal("deleted key must not be readable")
	}
	if cache.Delete("a") {
		t.Fatal("second Delete should report false")
	}
}

func TestKeysOrderFollowsPuts(t *testing.T) {
	cache := New(3)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3)

	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"c", "b", "a"}) {
		t.Fatalf("Keys() = %v, want [c b a]", keys)
	}
}

func TestGetRefreshesRecency(t *testing.T) {
	cache := New(2)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Get("a")
	cache.Put("c", 3)

	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"c", "a"}) {
		t.Fatalf("Keys() = %v, want [c a]", keys)
	}
	if _, ok := cache.Get("b"); ok {
		t.Fatal("the least recently used key b should have been evicted")
	}
}

func TestOverwriteRefreshesRecency(t *testing.T) {
	cache := New(2)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("a", 10)
	cache.Put("c", 3)

	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"c", "a"}) {
		t.Fatalf("Keys() = %v, want [c a]", keys)
	}
	if value, ok := cache.Get("a"); !ok || value != 10 {
		t.Fatalf("Get(\"a\") = (%d, %v), want (10, true)", value, ok)
	}
	if _, ok := cache.Get("b"); ok {
		t.Fatal("the least recently used key b should have been evicted")
	}
}

func TestDeleteKeepsKeysAndLenConsistent(t *testing.T) {
	cache := New(3)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Delete("a")

	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"b"}) {
		t.Fatalf("Keys() = %v, want [b]", keys)
	}
	if cache.Len() != len(cache.Keys()) {
		t.Fatalf("Len() = %d, but Keys() has %d entries", cache.Len(), len(cache.Keys()))
	}

	cache.Put("c", 3)
	cache.Put("d", 4)
	if keys := cache.Keys(); !reflect.DeepEqual(keys, []string{"d", "c", "b"}) {
		t.Fatalf("Keys() = %v, want [d c b]", keys)
	}
}

func TestZeroCapacity(t *testing.T) {
	cache := New(0)
	cache.Put("a", 1)

	if _, ok := cache.Get("a"); ok {
		t.Fatal("zero-capacity cache must not retain any key")
	}
	if cache.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", cache.Len())
	}
	if keys := cache.Keys(); len(keys) != 0 {
		t.Fatalf("Keys() = %v, want []", keys)
	}
}

func TestCapacityOne(t *testing.T) {
	cache := New(1)
	cache.Put("a", 1)
	cache.Put("b", 2)

	if _, ok := cache.Get("a"); ok {
		t.Fatal("a should have been evicted once b was inserted")
	}
	if value, ok := cache.Get("b"); !ok || value != 2 {
		t.Fatalf("Get(\"b\") = (%d, %v), want (2, true)", value, ok)
	}
	if cache.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", cache.Len())
	}
}
