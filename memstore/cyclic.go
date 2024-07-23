package memstore

import (
	"container/list"
	"sync"
)

type CyclicBuffer[T any] struct {
	size int
	list *list.List
	lock sync.RWMutex
}

func NewCyclicBuffer[T any](size int) *CyclicBuffer[T] {
	return &CyclicBuffer[T]{
		size: size,
		list: list.New(),
	}
}

func (c *CyclicBuffer[T]) Add(v T) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.list.PushBack(v)
	if c.list.Len() > c.size {
		c.list.Remove(c.list.Front())
	}
}

func (c *CyclicBuffer[T]) Items() []T {
	c.lock.RLock()
	defer c.lock.RUnlock()
	arr := make([]T, 0, c.size)
	for e := c.list.Front(); e != nil; e = e.Next() {
		arr = append(arr, e.Value.(T))
	}
	return arr
}
