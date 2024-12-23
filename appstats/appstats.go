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

package appstats

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	"github.com/harishjp/goread/config"
	"github.com/harishjp/goread/memstore"
)

var (
	// RecordFraction is the fraction of requests to record.
	// Set to a number between 0.0 (none) and 1.0 (all).
	RecordFraction = 1.0

	// ShouldRecord is the function used to determine if recording will occur
	// for a given request. The default is to use RecordFraction.
	ShouldRecord = DefaultShouldRecord
)

const (
	serveURL   = "/_ah/stats/"
	detailsURL = serveURL + "details"
	fileURL    = serveURL + "file"
	staticURL  = serveURL + "static/"
)

func Init(router *mux.Router) {
	router.HandleFunc(serveURL, appstatsHandler)
}

// DefaultShouldRecord will record a request based on RecordFraction.
func DefaultShouldRecord(_ *http.Request) bool {
	if RecordFraction >= 1.0 {
		return true
	}

	return rand.Float64() < RecordFraction
}

// Context is a timing-aware context.Context.
type Context struct {
	context.Context
	header http.Header
	stats  *requestStats
}

// NewContext creates a new timing-aware context from req.
func NewContext(req *http.Request) Context {
	c := req.Context()
	var uname string
	var admin bool
	if u := config.GetSession(c); u != nil {
		uname = u.Name
		admin = u.Admin
	}
	return Context{
		Context: c,
		header:  req.Header,
		stats: &requestStats{
			User:   uname,
			Admin:  admin,
			Method: req.Method,
			Path:   req.URL.Path,
			Query:  req.URL.RawQuery,
			Start:  time.Now(),
		},
	}
}

var fullStatsCache = memstore.NewCache(1000)
var partStatsBuffer = memstore.NewCyclicBuffer[*requestStats](modulus)

func (c Context) save() {
	c.stats.Duration = time.Since(c.stats.Start)

	full := statsFull{
		Header: c.header,
		Stats:  c.stats,
	}
	fullKey := c.stats.FullKey()
	fullStatsCache.Add(fullKey, full)

	part := *c.stats
	for i := range part.RPCStats {
		part.RPCStats[i].StackData = ""
		part.RPCStats[i].In = ""
		part.RPCStats[i].Out = ""
	}
	partStatsBuffer.Add(&part)
}

// URL returns the appstats URL for the current request.
func (c Context) URL() string {
	u := url.URL{
		Path:     detailsURL,
		RawQuery: fmt.Sprintf("time=%v", c.stats.Start.Nanosecond()),
	}
	return u.String()
}

// handler is an http.Handler that records RPC statistics.
type handler struct {
	f func(context.Context, http.ResponseWriter, *http.Request)
}

// NewHandler returns a new Handler that will execute f.
func NewHandler(f func(context.Context, http.ResponseWriter, *http.Request)) http.Handler {
	return handler{
		f: f,
	}
}

// NewHandlerFunc returns a new HandlerFunc that will execute f.
func NewHandlerFunc(f func(context.Context, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := handler{
			f: f,
		}
		h.ServeHTTP(w, r)
	}
}

type responseWriter struct {
	http.ResponseWriter

	c Context
}

func (r responseWriter) Write(b []byte) (int, error) {
	// Emulate the behavior of http.ResponseWriter.Write since it doesn't
	// call our WriteHeader implementation.
	if r.c.stats.Status == 0 {
		r.WriteHeader(http.StatusOK)
	}

	return r.ResponseWriter.Write(b)
}

func (r responseWriter) WriteHeader(i int) {
	r.c.stats.Status = i
	r.ResponseWriter.WriteHeader(i)
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if ShouldRecord(r) {
		c := NewContext(r)
		rw := responseWriter{
			ResponseWriter: w,
			c:              c,
		}
		h.f(c, rw, r)
		c.save()
	} else {
		h.f(r.Context(), w, r)
	}
}
