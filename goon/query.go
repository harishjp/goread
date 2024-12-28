/*
 * Copyright (c) 2012 Matt Jibson <matt.jibson@gmail.com>
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package goon

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/datastore/apiv1/datastorepb"
	"google.golang.org/api/iterator"
)

// Count returns the number of results for the query.
func (g *Goon) Count(q *datastore.Query) (int, error) {
	result, err := g.client.RunAggregationQuery(g.Context, q.NewAggregationQuery().WithCount("query_count"))
	if err != nil {
		return -1, err
	}
	if res, ok := result["query_count"].(*datastorepb.Value); ok {
		return int(res.GetIntegerValue()), nil
	}
	return -1, nil
}

// GetAll runs the query and returns all the keys that match the query, as well
// as appending the values to dst, setting the goon key fields of dst, and
// caching the returned data in local memory.
//
// For "keys-only" queries dst can be nil, however if it is not, then GetAll
// appends zero value structs to dst, only setting the goon key fields.
// No data is cached with "keys-only" queries.
//
// See: https://developers.google.com/appengine/docs/go/datastore/reference#Query.GetAll
func (g *Goon) GetAll(q *datastore.Query, dst interface{}, keysOnly bool) ([]*datastore.Key, error) {
	if dst == nil {
		return g.client.GetAll(g.Context, q, dst)
	}

	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("goon: Expected dst to be a pointer to a slice or nil, got instead: %v", v.Kind())
	}

	v = v.Elem()
	if v.Kind() != reflect.Slice {
		return nil, fmt.Errorf("goon: Expected dst to be a pointer to a slice or nil, got instead: %v", v.Kind())
	}

	vLenBefore := v.Len()

	elemType := v.Type().Elem()
	ptr := false
	if elemType.Kind() == reflect.Ptr {
		elemType = elemType.Elem()
		ptr = true
	}

	if elemType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("goon: Expected struct, got instead: %v", elemType.Kind())
	}

	var keys []*datastore.Key
	it := g.client.Run(g.Context, q)
	for {
		ev := reflect.New(elemType)
		key, err := it.Next(ev.Interface())
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		if !ptr {
			ev = ev.Elem()
		}
		v.Set(reflect.Append(v, ev))
	}
	if len(keys) == 0 {
		return keys, nil
	}
	updateCache := !keysOnly && g.txInfo == nil
	if updateCache {
		g.cacheLock.Lock()
		defer g.cacheLock.Unlock()
	}

	for i, k := range keys {
		var e interface{}
		vi := v.Index(vLenBefore + i)
		if vi.Kind() == reflect.Ptr {
			e = vi.Interface()
		} else {
			e = vi.Addr().Interface()
		}

		if err := g.setStructKey(e, k); err != nil {
			return nil, err
		}

		if updateCache {
			// Cache lock is handled before the for loop
			g.cache[memkey(k)] = e
		}
	}

	return keys, nil
}

func (g *Goon) RunNoCache(c context.Context, q *datastore.Query) *datastore.Iterator {
	return g.client.Run(c, q)
}

// Run runs the query.
func (g *Goon) Run(q *datastore.Query) *Iterator {
	return &Iterator{
		g: g,
		i: g.client.Run(g.Context, q),
	}
}

// Iterator is the result of running a query.
type Iterator struct {
	g *Goon
	i *datastore.Iterator
}

// Cursor returns a cursor for the iterator's current location.
func (t *Iterator) Cursor() (datastore.Cursor, error) {
	return t.i.Cursor()
}

// Next returns the entity of the next result. When there are no more results,
// iterator.Done is returned as the error. If dst is null (for a keys-only
// query), nil is returned as the entity.
//
// If the query is not keys only and dst is non-nil, it also loads the entity
// stored for that key into the struct pointer dst, with the same semantics
// and possible errors as for the Get function. This result is cached in memory.
//
// If the query is keys only, dst must be passed as nil. Otherwise the cache
// will be populated with empty entities since there is no way to detect the
// case of a keys-only query.
//
// Refer to appengine/datastore.Iterator.Next:
// https://developers.google.com/appengine/docs/go/datastore/reference#Iterator.Next
func (t *Iterator) Next(dst interface{}) (*datastore.Key, error) {
	k, err := t.i.Next(dst)
	if err != nil {
		return k, err
	}

	if dst != nil {
		// Update the struct to have correct key info
		err := t.g.setStructKey(dst, k)
		if err == nil && t.g.txInfo == nil {
			t.g.cacheLock.Lock()
			t.g.cache[memkey(k)] = dst
			t.g.cacheLock.Unlock()
		}
	}

	return k, err
}
