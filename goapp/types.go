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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"io"
	"net/url"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/harishjp/goread/config"
	"github.com/harishjp/goread/goon"
	"github.com/harishjp/goread/log"
	"github.com/harishjp/goread/task"
)

type User struct {
	_kind    string    `goon:"kind,U"`
	Id       string    `datastore:"-" goon:"id"`
	Email    string    `datastore:"e"`
	Messages []string  `datastore:"m,noindex"`
	Read     time.Time `datastore:"r"`
	Options  string    `datastore:"o,noindex"`
	Account  int       `datastore:"a"`
	Created  time.Time `datastore:"d"`
	Until    time.Time `datastore:"u"`
}

const (
	AFree = iota
	APaid
)

func (u *User) String() string {
	return u.Email
}

// UserData parent: User, key: "data"
type UserData struct {
	_kind  string         `goon:"kind,UD"`
	Id     string         `datastore:"-" goon:"id"`
	Parent *datastore.Key `datastore:"-" goon:"parent"`
	Opml   []byte         `datastore:"o,noindex"`
	Read   []byte         `datastore:"r,noindex"`
}

type UserStarFeed struct {
	_kind  string         `goon:"kind,USF"`
	Id     string         `datastore:"-" goon:"id"`
	Parent *datastore.Key `datastore:"-" goon:"parent"`
}

// UserStar parent: UserStarFeed, key: Story.Key.Encode()
type UserStar struct {
	_kind   string         `goon:"kind,US"`
	Id      string         `datastore:"-" goon:"id"`
	Parent  *datastore.Key `datastore:"-" goon:"parent"`
	Created time.Time      `datastore:"c"`
}

func starKey(c context.Context, feed, story string) *UserStar {
	cu := config.GetSession(c)
	gn := goon.FromContext(c)
	u := User{Id: cu.ID}
	uk := gn.Key(&u)
	return &UserStar{
		Parent: datastore.NameKey("USF", feed, uk),
		Id:     story,
	}
}

func starID(key *datastore.Key) string {
	return fmt.Sprintf("%s|%s", key.Parent.Name, key.Name)
}

type Read map[string]map[string]bool

func DecodeRead(b []byte) Read {
	r := make(Read)
	if len(b) > 0 {
		err := gob.NewDecoder(bytes.NewReader(b)).Decode(&r)
		if err != nil {
			log.Errorf(context.Background(), "goon.DecodeRead: %v", err)
		}
	}
	return r
}

func (r Read) Encode() []byte {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(r)
	if err != nil {
		log.Errorf(context.Background(), "goon.Encode: %v", err)
	}
	return buf.Bytes()
}

func (r Read) Set(feed, story string) {
	if m, ok := r[feed]; ok {
		m[story] = true
	} else {
		r[feed] = map[string]bool{story: true}
	}
}

func (r Read) Del(feed, story string) {
	if m, ok := r[feed]; ok {
		delete(m, story)
	}
}

func (r Read) Get(feed, story string) bool {
	if m, ok := r[feed]; ok {
		return m[story]
	}
	return false
}

type Feed struct {
	_kind      string        `goon:"kind,F"`
	Url        string        `datastore:"-" goon:"id"`
	Title      string        `datastore:"t,noindex"`
	Updated    time.Time     `datastore:"u,noindex" json:"-"`
	Date       time.Time     `datastore:"d,noindex" json:"-"`
	Checked    time.Time     `datastore:"c,noindex"`
	NextUpdate time.Time     `datastore:"n"`
	Link       string        `datastore:"l,noindex"`
	Hub        string        `datastore:"h,noindex" json:"-"`
	Errors     int           `datastore:"e,noindex"`
	Image      string        `datastore:"i,noindex"`
	ImageDate  time.Time     `datastore:"g,noindex"`
	Subscribed time.Time     `datastore:"s,noindex" json:"-"`
	Average    time.Duration `datastore:"a,noindex" json:"-"`
	LastViewed time.Time     `datastore:"v" json:"-"`
}

func (f *Feed) Subscribe(c context.Context) {
	if !f.IsSubscribed() {
		log.Infof(c, "Subscribe %v", f.Subscribed.String())
		err := task.SubmitTask(c, routeUrl("subscribe-feed"), url.Values{
			"feed": {f.Url},
		}, "update-manual")
		if err != nil {
			log.Errorf(c, "error submiting task: %v", err.Error())
		}
	}
}

func (f *Feed) IsSubscribed() bool {
	return true
}

func (f *Feed) PubSubURL() string {
	b := base64.URLEncoding.EncodeToString([]byte(f.Url))
	ru, _ := getURL("subscribe-callback")
	ru.Scheme = "http"
	ru.Host = ""
	ru.RawQuery = url.Values{
		"feed": {b},
	}.Encode()
	return ru.String()
}

func (f *Feed) NotViewed() bool {
	return time.Since(f.LastViewed) > notViewedDisabled
}

// Story parent: Feed, key: story ID
type Story struct {
	_kind        string         `goon:"kind,S"`
	Id           string         `datastore:"-" goon:"id"`
	Parent       *datastore.Key `datastore:"-" goon:"parent" json:"-"`
	Title        string         `datastore:"t,noindex"`
	Link         string         `datastore:"l,noindex"`
	Created      time.Time      `datastore:"c"`
	Published    time.Time      `datastore:"p,noindex" json:"-"`
	Updated      time.Time      `datastore:"u,noindex" json:"-"`
	Date         int64          `datastore:"e,noindex"`
	Author       string         `datastore:"a,noindex" json:",omitempty"`
	Summary      string         `datastore:"s,noindex"`
	MediaContent string         `datastore:"m,noindex" json:",omitempty"`

	content string
}

const IDX_COL = "c"

// StoryContent parent: Story, key: 1
type StoryContent struct {
	_kind      string         `goon:"kind,SC"`
	Id         int64          `datastore:"-" goon:"id"`
	Parent     *datastore.Key `datastore:"-" goon:"parent"`
	Content    string         `datastore:"c,noindex"`
	Compressed []byte         `datastore:"z,noindex"`
}

func (sc *StoryContent) content() string {
	if len(sc.Compressed) > 0 {
		buf := bytes.NewReader(sc.Compressed)
		if gz, err := gzip.NewReader(buf); err == nil {
			defer gz.Close()
			if b, err := io.ReadAll(gz); err == nil {
				return string(b)
			}
		}
	}
	return sc.Content
}

type OpmlOutline struct {
	Outline []*OpmlOutline `xml:"outline" json:",omitempty"`
	Title   string         `xml:"title,attr,omitempty" json:",omitempty"`
	XmlUrl  string         `xml:"xmlUrl,attr,omitempty" json:",omitempty"`
	Type    string         `xml:"type,attr,omitempty" json:",omitempty"`
	Text    string         `xml:"text,attr,omitempty" json:",omitempty"`
	HtmlUrl string         `xml:"htmlUrl,attr,omitempty" json:",omitempty"`
}

type Opml struct {
	XMLName string         `xml:"opml"`
	Version string         `xml:"version,attr"`
	Title   string         `xml:"head>title"`
	Outline []*OpmlOutline `xml:"body>outline"`
}

type Stories []*Story

func (s Stories) Len() int           { return len(s) }
func (s Stories) Less(i, j int) bool { return s[i].Created.Before(s[j].Created) }
func (s Stories) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type Log struct {
	_kind  string         `goon:"kind,L"`
	Id     int64          `datastore:"-" goon:"id"`
	Parent *datastore.Key `datastore:"-" goon:"parent"`
	Text   string         `datastore:"t,noindex"`
}
