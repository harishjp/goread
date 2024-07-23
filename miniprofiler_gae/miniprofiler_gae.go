/*
 * Copyright (c) 2013 Matt Jibson <matt.jibson@gmail.com>
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

package miniprofiler_gae

import (
	"net/http"

	"github.com/harishjp/goread/appstats"
	"github.com/harishjp/goread/memstore"
	"github.com/harishjp/goread/miniprofiler"
	"golang.org/x/net/context"
)

func init() {
	miniprofiler.Get = getCache
	miniprofiler.Store = storeCache
}

var cache = memstore.NewCache(100)

// storeCache stores the Profile in cache.
func storeCache(_ *http.Request, p *miniprofiler.Profile) {
	cache.Add(p.Id, p)
}

// getCache gets the Profile from cache.
func getCache(_ *http.Request, id string) *miniprofiler.Profile {
	profile, ok := cache.Get(id)
	if ok {
		return profile.(*miniprofiler.Profile)
	}
	return nil
}

type Context struct {
	appstats.Context
	miniprofiler.Timer
}

/*
func (c Context) Call(service, method string, in, out internal.Proto.Message) (err error) {
	if c.Timer != nil && service != "__go__" {
		c.StepCustomTiming(service, method, fmt.Sprintf("%v\n\n%v", method, in.String()), func() {
			err = c.Context.Call(service, method, in, out)
		})
	} else {
		err = c.Context.Call(service, method, in, out)
	}
	return
}*/

func (c Context) Step(name string, f func(Context)) {
	if c.Timer != nil {
		c.Timer.Step(name, func(t miniprofiler.Timer) {
			f(Context{
				Context: c.Context,
				Timer:   t,
			})
		})
	} else {
		f(c)
	}
}

// NewHandler returns a profiled, appstats-aware appengine.Context.
func NewHandler(f func(Context, http.ResponseWriter, *http.Request)) http.Handler {
	return appstats.NewHandler(func(c context.Context, w http.ResponseWriter, r *http.Request) {
		h := miniprofiler.NewHandler(func(t miniprofiler.Timer, w http.ResponseWriter, r *http.Request) {
			pc := Context{
				Context: c.(appstats.Context),
				Timer:   t,
			}
			t.SetName(miniprofiler.FuncName(f))
			f(pc, w, r)
			t.AddCustomLink("appstats", pc.URL())
		})
		h.ServeHTTP(w, r)
	})
}
