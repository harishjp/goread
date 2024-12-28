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
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	"github.com/harishjp/goread/config"
	"github.com/harishjp/goread/memstore"
)

const (
	serveURL   = "/_ah/stats/"
	detailsURL = serveURL + "details"
	fileURL    = serveURL + "file"
	staticURL  = serveURL + "static/"
)

func Init(router *mux.Router) {
	router.PathPrefix(serveURL).HandlerFunc(appstatsHandler)
}

type Stats struct {
	requestStats
}

// NewStats creates Stats and ResponseWriter which wraps and updates the stats for status.
func NewStats(w http.ResponseWriter, r *http.Request) (*Stats, http.ResponseWriter) {
	var uname string
	var admin bool
	if u := config.GetSession(r.Context()); u != nil {
		uname = u.Name
		admin = u.Admin
	}
	stats := Stats{requestStats{
		User:   uname,
		Admin:  admin,
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Start:  time.Now(),
	}}

	rw := responseWriter{
		ResponseWriter: w,
		s:              &stats.requestStats,
	}
	return &stats, rw
}

var fullStatsCache = memstore.NewCache(1000)
var partStatsBuffer = memstore.NewCyclicBuffer[*requestStats](modulus)

func (s *Stats) Save(r *http.Request) {
	s.Duration = time.Since(s.Start)

	full := statsFull{
		Header: r.Header,
		Stats:  &s.requestStats,
	}
	fullKey := s.FullKey()
	fullStatsCache.Add(fullKey, full)

	part := s.requestStats
	for i := range part.RPCStats {
		part.RPCStats[i].StackData = ""
		part.RPCStats[i].In = ""
		part.RPCStats[i].Out = ""
	}
	partStatsBuffer.Add(&part)
}

// URL returns the appstats URL for the current request.
func (s *Stats) URL() string {
	u := url.URL{
		Path:     detailsURL,
		RawQuery: fmt.Sprintf("time=%v", s.Start.Nanosecond()),
	}
	return u.String()
}

type responseWriter struct {
	http.ResponseWriter
	s *requestStats
}

func (r responseWriter) Write(b []byte) (int, error) {
	// Emulate the behavior of http.ResponseWriter.Write since it doesn't
	// call our WriteHeader implementation.
	if r.s.Status == 0 {
		r.WriteHeader(http.StatusOK)
	}

	return r.ResponseWriter.Write(b)
}

func (r responseWriter) WriteHeader(i int) {
	r.s.Status = i
	r.ResponseWriter.WriteHeader(i)
}
