// Package lrucache 提供一个固定容量的 LRU 缓存。
package lrucache

import "container/list"

type entry struct {
	key   string
	value int
}

// Cache 是固定容量的最近最少使用缓存。
type Cache struct {
	capacity int
	items    map[string]*list.Element
	order    *list.List // 头部是最新使用，尾部是最久未使用
}

// New 创建一个容量为 capacity 的缓存。
func New(capacity int) *Cache {
	return &Cache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get 读取键对应的值。
func (c *Cache) Get(key string) (int, bool) {
	element, ok := c.items[key]
	if !ok {
		return 0, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*entry).value, true
}

// Put 写入键值；容量满时淘汰最久未使用的键。
func (c *Cache) Put(key string, value int) {
	if element, ok := c.items[key]; ok {
		element.Value.(*entry).value = value
		c.order.MoveToFront(element)
		return
	}
	element := c.order.PushFront(&entry{key: key, value: value})
	c.items[key] = element
	for len(c.items) > c.capacity {
		c.removeOldest()
	}
}

func (c *Cache) removeOldest() {
	element := c.order.Back()
	if element == nil {
		return
	}
	c.order.Remove(element)
	delete(c.items, element.Value.(*entry).key)
}

// Delete 删除键，返回是否删掉了东西。
func (c *Cache) Delete(key string) bool {
	element, ok := c.items[key]
	if !ok {
		return false
	}
	c.order.Remove(element)
	delete(c.items, key)
	return true
}

// Len 返回当前键数量。
func (c *Cache) Len() int {
	return len(c.items)
}

// Keys 按从新到旧返回所有键。
func (c *Cache) Keys() []string {
	keys := make([]string, 0, c.order.Len())
	for element := c.order.Front(); element != nil; element = element.Next() {
		keys = append(keys, element.Value.(*entry).key)
	}
	return keys
}
