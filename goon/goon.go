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
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"

	"github.com/harishjp/goread/log"
	"github.com/harishjp/goread/memstore"

	"golang.org/x/net/context"

	"cloud.google.com/go/datastore"
)

// This is global cache to replace memcache.
var goonCache = memstore.NewCache(2000)

type pendingPut struct {
	key   *datastore.PendingKey
	value any
}

type TxInfo struct {
	tx         *datastore.Transaction
	toSet      []pendingPut
	toDelete   map[string]bool
	toDeleteMC []string
}

// KindNameResolver takes an Entity and returns what the Kind should be for
// Datastore.
type KindNameResolver func(src interface{}) string

// Goon holds the app engine context and the request memory cache.
type Goon struct {
	Context   context.Context
	client    *datastore.Client
	cache     map[string]interface{}
	cacheLock sync.RWMutex // protect the cache from concurrent goroutines to speed up RPC access
	// KindNameResolver is used to determine what Kind to give an Entity.
	// Defaults to DefaultKindName
	KindNameResolver KindNameResolver

	txInfo *TxInfo
}

func memkey(k *datastore.Key) string {
	// Versioning, so that incompatible changes to the cache system won't cause problems
	return "g2:" + k.Encode()
}

var clientKey = struct{}{}

func ContextWithClient(ctx context.Context, client *datastore.Client) context.Context {
	return context.WithValue(ctx, clientKey, client)
}

// FromContext creates a new Goon object from the given context Context.
// Useful with profiling packages like appstats.
func FromContext(c context.Context) *Goon {
	client, ok := c.Value(clientKey).(*datastore.Client)
	if !ok {
		panic("client not found in context")
	}
	return &Goon{
		Context:          c,
		client:           client,
		cache:            make(map[string]interface{}),
		KindNameResolver: DefaultKindName,
	}
}

func (g *Goon) error(err error) {
	_, filename, line, ok := runtime.Caller(1)
	if ok {
		log.Errorf(g.Context, "goon - %s:%d - %v", filepath.Base(filename), line, err)
	} else {
		log.Errorf(g.Context, "goon - %v", err)
	}
}

func (g *Goon) extractKeys(src interface{}, putRequest bool) ([]*datastore.Key, error) {
	v := reflect.Indirect(reflect.ValueOf(src))
	if v.Kind() != reflect.Slice {
		return nil, fmt.Errorf("goon: value must be a slice or pointer-to-slice")
	}
	l := v.Len()

	keys := make([]*datastore.Key, l)
	for i := 0; i < l; i++ {
		vi := v.Index(i)
		key, hasStringId, err := g.getStructKey(vi.Interface())
		if err != nil {
			return nil, err
		}
		if !putRequest && key.Incomplete() {
			return nil, fmt.Errorf("goon: cannot find a key for struct - %v", vi.Interface())
		} else if putRequest && key.Incomplete() && hasStringId {
			return nil, fmt.Errorf("goon: empty string id on put")
		}
		keys[i] = key
	}
	return keys, nil
}

// Key is the same as KeyError, except nil is returned on error or if the key
// is incomplete.
func (g *Goon) Key(src interface{}) *datastore.Key {
	if k, err := g.KeyError(src); err == nil {
		return k
	}
	return nil
}

// Kind returns src's datastore Kind or "" on error.
func (g *Goon) Kind(src interface{}) string {
	if k, err := g.KeyError(src); err == nil {
		return k.Kind
	}
	return ""
}

// KeyError returns the key of src based on its properties.
func (g *Goon) KeyError(src interface{}) (*datastore.Key, error) {
	key, _, err := g.getStructKey(src)
	return key, err
}

// RunInTransaction runs f in a transaction. It calls f with a transaction
// context tg that f should use for all App Engine operations. Neither cache nor
// memcache are used or set during a transaction.
//
// Otherwise similar to appengine/datastore.RunInTransaction:
// https://developers.google.com/appengine/docs/go/datastore/reference#RunInTransaction
func (g *Goon) RunInTransaction(f func(g *Goon) error, opts ...datastore.TransactionOption) error {
	txInfo := TxInfo{
		toSet:      nil,
		toDelete:   make(map[string]bool),
		toDeleteMC: nil,
	}
	commit, err := g.client.RunInTransaction(g.Context, func(tx *datastore.Transaction) error {
		txInfo.tx = tx
		return f(&Goon{
			Context:          g.Context,
			client:           g.client,
			KindNameResolver: g.KindNameResolver,
			txInfo:           &txInfo,
		})
	}, opts...)

	if err == nil {
		goonCache.Remove(txInfo.toDeleteMC)

		g.cacheLock.Lock()
		defer g.cacheLock.Unlock()
		for _, p := range txInfo.toSet {
			key := commit.Key(p.key)
			if g.setStructKey(p.value, key) != nil {
				mk := memkey(key)
				g.cache[mk] = p.value
			}
		}

		for k := range txInfo.toDelete {
			delete(g.cache, k)
		}
	} else {
		g.error(err)
	}

	return err
}

// Put saves the entity src into the datastore based on src's key k. If k
// is an incomplete key, the returned key will be a unique key generated by
// the datastore.
func (g *Goon) Put(src interface{}) (*datastore.Key, error) {
	ks, err := g.PutMulti([]interface{}{src})
	if err != nil {
		var me datastore.MultiError
		if errors.As(err, &me) {
			return nil, me[0]
		}
		return nil, err
	}
	return ks[0], nil
}

const putMultiLimit = 500

// PutMulti is a batch version of Put.
//
// src must be a *[]S, *[]*S, *[]I, []S, []*S, or []I, for some struct type S,
// or some interface type I. If *[]I or []I, each element must be a struct pointer.
func (g *Goon) PutMulti(src interface{}) ([]*datastore.Key, error) {
	keys, err := g.extractKeys(src, true) // allow incomplete keys on a Put request
	if err != nil {
		return nil, err
	}

	var memkeys []string
	for _, key := range keys {
		if !key.Incomplete() {
			memkeys = append(memkeys, memkey(key))
		}
	}

	// cache needs to be updated after the datastore to prevent a common race condition,
	// where a concurrent request will fetch the not-yet-updated data from the datastore
	// and populate cache with it.
	if g.txInfo != nil {
		g.txInfo.toDeleteMC = append(g.txInfo.toDeleteMC, memkeys...)
	} else {
		defer goonCache.Remove(memkeys)
	}

	v := reflect.Indirect(reflect.ValueOf(src))
	multiErr, anyErr := make(datastore.MultiError, len(keys)), false
	goroutines := (len(keys)-1)/putMultiLimit + 1
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			lo := i * putMultiLimit
			hi := (i + 1) * putMultiLimit
			if hi > len(keys) {
				hi = len(keys)
			}

			var pmerr error
			var pendingKeys []*datastore.PendingKey
			var rkeys []*datastore.Key
			if g.txInfo != nil {
				pendingKeys, pmerr = g.txInfo.tx.PutMulti(keys[lo:hi], v.Slice(lo, hi).Interface())
			} else {
				rkeys, pmerr = g.client.PutMulti(g.Context, keys[lo:hi], v.Slice(lo, hi).Interface())
			}

			if pmerr != nil {
				anyErr = true // this flag tells PutMulti to return multiErr later
				var merr datastore.MultiError
				if !errors.As(pmerr, &merr) {
					g.error(pmerr)
					for j := lo; j < hi; j++ {
						multiErr[j] = pmerr
					}
					return
				}
				copy(multiErr[lo:hi], merr)
			}

			if g.txInfo != nil {
				for i, key := range pendingKeys[lo:hi] {
					if multiErr[lo+i] != nil {
						continue // there was an error writing this value, go to next
					}
					vi := v.Index(lo + i).Interface()
					g.txInfo.toSet = append(g.txInfo.toSet, pendingPut{
						key:   key,
						value: vi,
					})
				}
			} else {
				for i, key := range keys[lo:hi] {
					if multiErr[lo+i] != nil {
						continue // there was an error writing this value, go to next
					}
					vi := v.Index(lo + i).Interface()
					if key.Incomplete() {
						err = g.setStructKey(vi, rkeys[i])
						keys[i] = rkeys[i]
						if err != nil {
							continue
						}
					}
					g.putMemory(vi, false)
				}
			}
		}(i)
	}
	wg.Wait()
	if anyErr {
		return keys, realError(multiErr)
	}
	return keys, nil
}

func (g *Goon) putMemoryMulti(src interface{}, exists []byte, addToGlobal bool) {
	v := reflect.Indirect(reflect.ValueOf(src))
	for i := 0; i < v.Len(); i++ {
		if exists[i] == 0 {
			continue
		}
		g.putMemory(v.Index(i).Interface(), addToGlobal)
	}
}

func (g *Goon) putMemory(src interface{}, addToGlobal bool) {
	key, _, _ := g.getStructKey(src)
	mkey := memkey(key)
	g.cacheLock.Lock()
	g.cache[mkey] = src
	g.cacheLock.Unlock()
	if addToGlobal {
		goonCache.Add(mkey, src)
	}
}

// FlushLocalCache clears the local memory cache.
func (g *Goon) FlushLocalCache() {
	g.cacheLock.Lock()
	g.cache = make(map[string]interface{})
	g.cacheLock.Unlock()
}

func (g *Goon) putMemcache(srcs []interface{}, exists []byte) {
	g.putMemoryMulti(srcs, exists, true)
}

// Get loads the entity based on dst's key into dst
// If there is no such entity for the key, Get returns
// datastore.ErrNoSuchEntity.
func (g *Goon) Get(dst interface{}) error {
	set := reflect.ValueOf(dst)
	if set.Kind() != reflect.Ptr {
		return fmt.Errorf("goon: expected pointer to a struct, got %#v", dst)
	}
	if !set.CanSet() {
		set = set.Elem()
	}
	dsts := []interface{}{dst}
	if err := g.GetMulti(dsts); err != nil {
		// Look for an embedded error if it's multi
		var me datastore.MultiError
		if errors.As(err, &me) {
			return me[0]
		}
		// Not multi, normal error
		return err
	}
	set.Set(reflect.Indirect(reflect.ValueOf(dsts[0])))
	return nil
}

const getMultiLimit = 1000

// GetMulti is a batch version of Get.
//
// dst must be a *[]S, *[]*S, *[]I, []S, []*S, or []I, for some struct type S,
// or some interface type I. If *[]I or []I, each element must be a struct pointer.
func (g *Goon) GetMulti(dst interface{}) error {
	keys, err := g.extractKeys(dst, false) // don't allow incomplete keys on a Get request
	if err != nil {
		return err
	}

	v := reflect.Indirect(reflect.ValueOf(dst))

	if g.txInfo != nil {
		// todo: support getMultiLimit in transactions
		return g.txInfo.tx.GetMulti(keys, v.Interface())
	}

	var dskeys []*datastore.Key
	var dsdst []interface{}
	var dixs []int

	g.cacheLock.RLock()
	for i, key := range keys {
		m := memkey(key)
		vi := v.Index(i)

		if vi.Kind() == reflect.Struct {
			vi = vi.Addr()
		}

		if s, present := g.cache[m]; present {
			if vi.Kind() == reflect.Interface {
				vi = vi.Elem()
			}
			reflect.Indirect(vi).Set(reflect.Indirect(reflect.ValueOf(s)))
		} else if el, ok := goonCache.Get(m); ok {
			if vi.Kind() == reflect.Interface {
				vi = vi.Elem()
			}
			reflect.Indirect(vi).Set(reflect.Indirect(reflect.ValueOf(el)))
		} else {
			dskeys = append(dskeys, key)
			dsdst = append(dsdst, vi.Interface())
			dixs = append(dixs, i)
		}
	}
	g.cacheLock.RUnlock()

	if len(dskeys) == 0 {
		return nil
	}

	multiErr := make(datastore.MultiError, len(keys))
	anyErr := false
	goroutines := (len(dskeys)-1)/getMultiLimit + 1
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			var toCache []interface{}
			var exists []byte
			lo := i * getMultiLimit
			hi := (i + 1) * getMultiLimit
			if hi > len(dskeys) {
				hi = len(dskeys)
			}
			var gmerr error
			if g.txInfo != nil {
				gmerr = g.txInfo.tx.GetMulti(dskeys[lo:hi], dsdst[lo:hi])
			} else {
				gmerr = g.client.GetMulti(g.Context, dskeys[lo:hi], dsdst[lo:hi])
			}
			if gmerr != nil {
				anyErr = true // this flag tells GetMulti to return multiErr later
				var merr datastore.MultiError
				if !errors.As(gmerr, &merr) {
					g.error(gmerr)
					for j := lo; j < hi; j++ {
						multiErr[j] = gmerr
					}
					return
				}
				for i, idx := range dixs[lo:hi] {
					if merr[i] == nil {
						toCache = append(toCache, dsdst[lo+i])
						exists = append(exists, 1)
					} else {
						if errors.Is(merr[i], datastore.ErrNoSuchEntity) {
							toCache = append(toCache, dsdst[lo+i])
							exists = append(exists, 0)
						}
						multiErr[idx] = merr[i]
					}
				}
			} else {
				toCache = append(toCache, dsdst[lo:hi]...)
				exists = append(exists, bytes.Repeat([]byte{1}, hi-lo)...)
			}
			if len(toCache) > 0 {
				g.putMemcache(toCache, exists)
			}
		}(i)
	}
	wg.Wait()
	if anyErr {
		return realError(multiErr)
	}
	return nil
}

// Delete deletes the entity for the given key.
func (g *Goon) Delete(key *datastore.Key) error {
	keys := []*datastore.Key{key}
	err := g.DeleteMulti(keys)
	var me datastore.MultiError
	if errors.As(err, &me) {
		return me[0]
	}
	return err
}

const deleteMultiLimit = 500

// Returns a single error if each error in MultiError is the same
// otherwise, returns multiError or nil (if multiError is empty)
func realError(multiError datastore.MultiError) error {
	if len(multiError) == 0 {
		return nil
	}
	init := multiError[0]
	for i := 1; i < len(multiError); i++ {
		// since type error could hold structs, pointers, etc,
		// the only way to compare non-nil errors is by their string output
		if init == nil || multiError[i] == nil {
			if !errors.Is(init, multiError[i]) {
				return multiError
			}
		} else if init.Error() != multiError[i].Error() {
			return multiError
		}
	}
	// all errors are the same
	// some errors are *always* returned in MultiError form from the datastore
	var errFieldMismatch *datastore.ErrFieldMismatch
	if errors.As(init, &errFieldMismatch) { // returned in GetMulti
		return multiError
	}
	if errors.Is(init, datastore.ErrInvalidEntityType) || // returned in GetMulti
		errors.Is(init, datastore.ErrNoSuchEntity) { // returned in GetMulti
		return multiError
	}
	// datastore.ErrInvalidKey is returned as a single error in PutMulti
	return init
}

// DeleteMulti is a batch version of Delete.
func (g *Goon) DeleteMulti(keys []*datastore.Key) error {
	if len(keys) == 0 {
		return nil
		// not an error, and it was "successful", so return nil
	}
	memkeys := make([]string, len(keys))

	g.cacheLock.Lock()
	for i, k := range keys {
		mk := memkey(k)
		memkeys[i] = mk
		if g.txInfo != nil {
			g.txInfo.toDelete[mk] = true
		} else {
			delete(g.cache, mk)
		}
	}
	g.cacheLock.Unlock()

	// Memcache needs to be updated after the datastore to prevent a common race condition,
	// where a concurrent request will fetch the not-yet-updated data from the datastore
	// and populate memcache with it.
	if g.txInfo != nil {
		g.txInfo.toDeleteMC = append(g.txInfo.toDeleteMC, memkeys...)
	} else {
		defer goonCache.Remove(memkeys)
	}

	multiErr, anyErr := make(datastore.MultiError, len(keys)), false
	goroutines := (len(keys)-1)/deleteMultiLimit + 1
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			lo := i * deleteMultiLimit
			hi := (i + 1) * deleteMultiLimit
			if hi > len(keys) {
				hi = len(keys)
			}
			var dmerr error
			if g.txInfo != nil {
				dmerr = g.txInfo.tx.DeleteMulti(keys[lo:hi])
			} else {
				dmerr = g.client.DeleteMulti(g.Context, keys[lo:hi])
			}
			if dmerr != nil {
				anyErr = true // this flag tells DeleteMulti to return multiErr later
				var merr datastore.MultiError
				if !errors.As(dmerr, &merr) {
					g.error(dmerr)
					for j := lo; j < hi; j++ {
						multiErr[j] = dmerr
					}
					return
				}
				copy(multiErr[lo:hi], merr)
			}
		}(i)
	}
	wg.Wait()
	if anyErr {
		return realError(multiErr)
	}
	return nil
}

// NotFound returns true if err is an datastore.MultiError and err[idx] is a datastore.ErrNoSuchEntity.
func NotFound(err error, idx int) bool {
	var merr datastore.MultiError
	if errors.As(err, &merr) {
		return idx < len(merr) && errors.Is(merr[idx], datastore.ErrNoSuchEntity)
	}
	return false
}
