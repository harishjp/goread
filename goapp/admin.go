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

package goread

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/harishjp/goread/goon"
	"github.com/harishjp/goread/log"
)

func AllFeedsOpml(w http.ResponseWriter, r *http.Request) {
	gn := goon.FromContext(r.Context())
	q := datastore.NewQuery(gn.Kind(&Feed{})).KeysOnly()
	keys, _ := gn.GetAll(q, nil, true)
	fs := make([]*Feed, len(keys))
	for i, k := range keys {
		fs[i] = &Feed{Url: k.Name}
	}
	b := feedsToOpml(fs)
	w.Header().Add("Content-Type", "text/xml")
	w.Header().Add("Content-Disposition", "attachment; filename=all.opml")
	w.Write(b)
}

func feedsToOpml(feeds []*Feed) []byte {
	opml := Opml{Version: "1.0"}
	opml.Outline = make([]*OpmlOutline, len(feeds))
	for i, f := range feeds {
		opml.Outline[i] = &OpmlOutline{
			XmlUrl:  f.Url,
			Type:    "rss",
			Text:    f.Title,
			Title:   f.Title,
			HtmlUrl: f.Link,
		}
	}
	b, _ := xml.Marshal(&opml)
	b = append([]byte(`<?xml version="1.0" encoding="UTF-8"?>`), b...)
	return b
}

func AllFeeds(w http.ResponseWriter, r *http.Request) {
	gn := goon.FromContext(r.Context())
	q := datastore.NewQuery(gn.Kind(&Feed{})).KeysOnly()
	keys, _ := gn.GetAll(q, nil, true)
	templates.ExecuteTemplate(w, "admin-all-feeds.html", keys)
}

func AdminFeed(w http.ResponseWriter, r *http.Request) {
	gn := goon.FromContext(r.Context())
	f := Feed{Url: r.FormValue("f")}
	if err := gn.Get(&f); err != nil {
		serveError(w, err)
		return
	}
	q := datastore.NewQuery(gn.Kind(&Story{})).KeysOnly()
	fk := gn.Key(&f)
	q = q.Ancestor(fk)
	q = q.Limit(100)
	q = q.Order("-" + IDX_COL)
	keys, _ := gn.GetAll(q, nil, true)
	stories := make([]*Story, len(keys))
	for j, key := range keys {
		stories[j] = &Story{
			Id:     key.Name,
			Parent: fk,
		}
	}
	gn.GetMulti(stories)

	templates.ExecuteTemplate(w, "admin-feed.html", struct {
		Feed    *Feed
		Stories []*Story
		Now     time.Time
	}{
		&f,
		stories,
		time.Now(),
	})
}

func AdminUpdateFeed(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	url := r.FormValue("f")
	if feed, stories, err := fetchFeed(c, url, url); err == nil {
		updateFeed(c, url, feed, stories, true, false, false)
		fmt.Fprintf(w, "updated: %v", url)
	} else {
		fmt.Fprintf(w, "error updating %v: %v", url, err)
	}
}

func AdminSubHub(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	gn := goon.FromContext(c)
	f := Feed{Url: r.FormValue("f")}
	if err := gn.Get(&f); err != nil {
		serveError(w, err)
		return
	}
	f.Subscribed = time.Time{}
	f.Subscribe(c)
	fmt.Fprintf(w, "subscribed")
}

func AdminDateFormats(w http.ResponseWriter, _ *http.Request) {
	dfs := make(map[string]df)
	for i, v := range invalidDateBuffer.Items() {
		dfs[fmt.Sprintf("_dateformat-%v", i)] = v
	}
	if err := templates.ExecuteTemplate(w, "admin-date-formats.html", dfs); err != nil {
		serveError(w, err)
	}
}

func AdminStats(w http.ResponseWriter, r *http.Request) {
	gn := goon.FromContext(r.Context())
	uc, _ := gn.Count(datastore.NewQuery(gn.Kind(&User{})))
	templates.ExecuteTemplate(w, "admin-stats.html", struct {
		Users int
	}{
		uc,
	})
}

func AdminUser(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	gn := goon.FromContext(c)
	q := datastore.NewQuery(gn.Kind(&User{})).Limit(1)
	q = q.FilterField("e", "=", r.FormValue("u"))
	it := gn.Run(q)
	var u User
	ud := UserData{Id: "data"}
	_, err := it.Next(&u)
	if err != nil {
		serveError(w, err)
		return
	}
	ud.Parent = gn.Key(&u)
	gn.Get(&ud)
	until := r.FormValue("until")
	if d, err := time.Parse("2006-01-02", until); err == nil {
		u.Until = d
		gn.Put(&u)
	}
	if o := []byte(r.FormValue("opml")); len(o) > 0 {
		opml := Opml{}
		if err := json.Unmarshal(o, &opml); err != nil {
			serveError(w, err)
			return
		}
		ud.Opml = o
		if _, err := gn.Put(&ud); err != nil {
			serveError(w, err)
			return
		}
		log.Infof(c, "opml updated")
	}
	if err := templates.ExecuteTemplate(w, "admin-user.html", struct {
		User User
		Data UserData
	}{
		u,
		ud,
	}); err != nil {
		serveError(w, err)
	}
}
