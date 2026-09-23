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
