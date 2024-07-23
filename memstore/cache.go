package memstore

import (
	"container/list"
	"sync"
)

type entry struct {
	key     string
	value   any
	visited bool
}

type Cache struct {
	cache   map[string]*list.Element
	entries *list.List
	curr    *list.Element
	size    int
	lock    sync.Mutex
}

// NewCache creates a cache which uses sieve algorithm: https://cachemon.github.io/SIEVE-website/
// Its thread safe but not read optimized with RWLock, visited would have to be atomic for that.
func NewCache(size int) *Cache {
	return &Cache{
		cache:   make(map[string]*list.Element),
		entries: list.New(),
		size:    size,
	}
}

func (c *Cache) Get(key string) (any, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if e, ok := c.cache[key]; ok {
		e.Value.(*entry).visited = true
		return e.Value.(*entry).value, true
	}
	return nil, false
}

func (c *Cache) Add(key string, val any) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if e, ok := c.cache[key]; ok {
		e.Value.(*entry).visited = true
		e.Value.(*entry).value = val
		return
	}

	c.cache[key] = c.entries.PushFront(&entry{
		key:   key,
		value: val,
	})

	c.removeExtra()
}

func (c *Cache) removeExtra() {
	if len(c.cache) <= c.size {
		return
	}

	if c.curr == nil {
		// This should never be nil because it's called after PushFront in Add
		c.curr = c.entries.Back()
	}

	el := c.curr
	en := el.Value.(*entry)

	// There is aleast one visited = false, because it is called after PushFront in Add
	for en.visited {
		en.visited = false
		el = el.Prev()
		en = el.Value.(*entry)
	}

	c.curr = el.Prev()
	delete(c.cache, en.key)
	c.entries.Remove(el)
}

func (c *Cache) Remove(keys []string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	for _, key := range keys {
		if el, ok := c.cache[key]; ok {
			if c.curr != nil && c.curr.Value.(*entry).key == key {
				c.curr = el.Prev()
			}
			delete(c.cache, key)
			c.entries.Remove(el)
		}
	}
}

func (c *Cache) Flush() {
	c.lock.Lock()
	c.cache = make(map[string]*list.Element)
	c.entries = list.New()
	c.curr = nil
	c.lock.Unlock()
}
