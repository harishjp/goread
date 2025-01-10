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
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/harishjp/goread/goon"
	"github.com/harishjp/goread/log"
	"github.com/harishjp/goread/task"
	"golang.org/x/net/html/charset"
	"google.golang.org/api/iterator"
)

func ImportOpmlTask(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	gn := goon.FromContext(c)
	userid := r.FormValue("user")
	filePath := r.FormValue("key")

	var skip int
	if s, err := strconv.Atoi(r.FormValue("skip")); err == nil {
		skip = s
	}
	log.Debugf(c, "reader import for %v, skip %v", userid, skip)

	file, err := os.Open(filePath)
	if err != nil {
		log.Warningf(c, "file open failed: %v", err.Error())
		_ = os.Remove(filePath)
		return
	}

	d := xml.NewDecoder(file)
	d.CharsetReader = charset.NewReaderLabel
	d.Strict = false
	opml := Opml{}
	err = d.Decode(&opml)
	if err != nil {
		log.Warningf(c, "gob decode failed: %v", err.Error())
		_ = os.Remove(filePath)
		return
	}

	remaining := skip
	var userOpml []*OpmlOutline
	var proc func(label string, outlines []*OpmlOutline)
	proc = func(label string, outlines []*OpmlOutline) {
		for _, o := range outlines {
			if o.Title == "" {
				o.Title = o.Text
			}
			if o.XmlUrl != "" {
				if remaining > 0 {
					remaining--
				} else if len(userOpml) < IMPORT_LIMIT {
					userOpml = append(userOpml, &OpmlOutline{
						Title:   label,
						Outline: []*OpmlOutline{o},
					})
				}
			}

			if o.Title != "" && len(o.Outline) > 0 {
				proc(o.Title, o.Outline)
			}
		}
	}

	proc("", opml.Outline)

	// todo: refactor below with similar from ImportReaderTask
	wg := sync.WaitGroup{}
	wg.Add(len(userOpml))
	for i := range userOpml {
		go func(i int) {
			o := userOpml[i].Outline[0]
			if err := addFeed(c, userid, userOpml[i]); err != nil {
				log.Warningf(c, "opml import error: %v", err.Error())
				// todo: do something here?
			}
			log.Debugf(c, "opml import: %s, %s", o.Title, o.XmlUrl)
			wg.Done()
		}(i)
	}
	wg.Wait()

	ud := UserData{Id: "data", Parent: gn.Key(&User{Id: userid})}
	if err := gn.RunInTransaction(func(gn *goon.Goon) error {
		gn.Get(&ud)
		if err := mergeUserOpml(c, &ud, userOpml...); err != nil {
			return err
		}
		_, err := gn.Put(&ud)
		return err
	}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf(c, "ud update error: %v", err.Error())
		_ = os.Remove(filePath)
		return
	}

	if len(userOpml) == IMPORT_LIMIT {
		err = task.SubmitTask(c, routeUrl("import-opml-task"), url.Values{
			"key":  {filePath},
			"user": {userid},
			"skip": {strconv.Itoa(skip + IMPORT_LIMIT)},
		}, "import-reader")
		if err != nil {
			log.Warningf(c, "error submitting task: %v", err)
		}
	} else {
		_ = os.Remove(filePath)
		log.Infof(c, "opml import done: %v", userid)
	}
}

const IMPORT_LIMIT = 10

func UpdateFeeds(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	q := datastore.NewQuery("F").KeysOnly().FilterField("n", "<=", time.Now())
	q = q.Limit(10 * 60 * 2) // 10/s queue, 2 min cron
	c1, cf := context.WithTimeout(c, time.Minute)
	defer cf()
	queue, err := task.NewCloudTaskQueue(c1)
	if err != nil {
		log.Errorf(c, "error creating queue: %v", err)
		return
	}
	defer queue.Close()
	it := goon.FromContext(c1).RunNoCache(c1, q)
	u := routeUrl("update-feed")
	i := 0
	for {
		k, err := it.Next(nil)
		if errors.Is(err, iterator.Done) {
			break
		} else if err != nil {
			log.Errorf(c, "next error: %v", err)
			break
		}
		err = queue.SubmitTask(c1, task.NewPostTask(u, url.Values{
			"feed": {k.Name},
		}, "update-feed"))
		if err != nil {
			log.Warningf(c, "error submitting task: %v", err)
		}
		i++
	}
	log.Infof(c, "updating %d feeds", i)
}

func fetchFeed(c context.Context, origUrl, fetchUrl string) (*Feed, []*Story, error) {
	u, err := url.Parse(fetchUrl)
	if err != nil {
		return nil, nil, err
	}
	if u.Host == "" {
		u.Host = u.Path
		u.Path = ""
	}
	const clURL = "craigslist.org"
	if strings.HasSuffix(u.Host, clURL) || u.Host == clURL {
		return nil, nil, fmt.Errorf("Craigslist blocks our server host: not possible to subscribe")
	}
	if u.Scheme == "" {
		u.Scheme = "http"
		origUrl = u.String()
		fetchUrl = origUrl
		if origUrl == "" {
			return nil, nil, fmt.Errorf("bad URL")
		}
	}

	httpContext, cf := context.WithTimeout(c, time.Minute)
	defer cf()
	if b, header, err := httpGet(httpContext, fetchUrl); err == nil {
		if autoUrl, err := Autodiscover(b); err == nil && origUrl == fetchUrl {
			if autoU, err := url.Parse(autoUrl); err == nil {
				if autoU.Scheme == "" {
					autoU.Scheme = u.Scheme
				}
				if autoU.Host == "" {
					autoU.Host = u.Host
				}
				autoUrl = autoU.String()
			}
			if autoUrl != fetchUrl {
				return fetchFeed(c, origUrl, autoUrl)
			}
		}
		return ParseFeed(c, header.Get("Content-Type"), origUrl, fetchUrl, b)
	} else {
		log.Warningf(c, "fetch feed error: %v", err)
		return nil, nil, fmt.Errorf("could not fetch feed")
	}
}

const maxGenerationCount = 100

func updateFeed(c context.Context, url string, feed *Feed, stories []*Story, updateAll, fromSub, updateLast bool) error {
	gn := goon.FromContext(c)
	f := Feed{Url: url}
	if err := gn.Get(&f); err != nil {
		return fmt.Errorf("feed not found: %s", url)
	}
	log.Infof(c, "feed update: %s", url)

	// Compare the feed's listed update to the story's update.
	// Note: these may not be accurate, hence, only compare them to each other,
	// since they should have the same relative error.
	storyDate := f.Updated

	hasUpdated := !feed.Updated.IsZero()
	isFeedUpdated := f.Updated.Equal(feed.Updated)
	if !hasUpdated {
		feed.Updated = f.Updated
	}
	feed.Date = f.Date
	feed.Average = f.Average
	feed.LastViewed = f.LastViewed
	feed.CurrGen = f.CurrGen
	feed.CurrGenCount = f.CurrGenCount
	f = *feed
	if updateLast {
		f.LastViewed = time.Now()
	}

	if hasUpdated && isFeedUpdated && !updateAll && !fromSub {
		log.Infof(c, "feed %s already updated to %v, putting", url, feed.Updated)
		f.Updated = time.Now()
		scheduleNextUpdate(c, &f)
		gn.Put(&f)
		return nil
	}

	log.Debugf(c, "feed: %s, hasUpdate: %v, isFeedUpdated: %v, storyDate: %v, stories: %v",
		url, hasUpdated, isFeedUpdated, storyDate, len(stories))
	puts := []interface{}{&f}

	// find non existant stories
	fk := gn.Key(&f)
	getStories := make([]*Story, len(stories))
	for i, s := range stories {
		getStories[i] = &Story{Id: s.Id, Parent: fk}
	}
	err := gn.GetMulti(getStories)
	var multiError datastore.MultiError
	if err != nil && !errors.As(err, &multiError) {
		log.Errorf(c, "GetMulti error: %s, %v", url, err)
		return err
	}
	var updateStories []*Story
	generationChanged := false
	addStoryToUpdate := func(f *Feed, story *Story) {
		story.Generation = f.CurrGen
		f.CurrGenCount += 1
		if f.CurrGenCount >= maxGenerationCount {
			f.CurrGenCount = 0
			f.CurrGen += 1
			generationChanged = true
		}
		updateStories = append(updateStories, story)
	}
	for i, s := range getStories {
		if goon.NotFound(err, i) {
			addStoryToUpdate(&f, stories[i])
		} else if (!stories[i].Updated.IsZero() && !stories[i].Updated.Equal(s.Updated)) || updateAll {
			if !s.Created.IsZero() {
				stories[i].Created = s.Created
			}
			if !s.Published.IsZero() {
				stories[i].Published = s.Published
			}
			addStoryToUpdate(&f, stories[i])
		}
	}
	log.Debugf(c, "feed: %s, %v update stories", url, len(updateStories))

	for _, s := range updateStories {
		puts = append(puts, s)
		sc := StoryContent{
			Id:     1,
			Parent: gn.Key(s),
		}
		buf := &bytes.Buffer{}
		if gz, err := gzip.NewWriterLevel(buf, gzip.BestCompression); err == nil {
			gz.Write([]byte(s.content))
			gz.Close()
			sc.Compressed = buf.Bytes()
		}
		if len(sc.Compressed) == 0 {
			sc.Content = s.content
		}
		if _, err := gn.Put(&sc); err != nil {
			log.Errorf(c, "put sc err: %v", err)
			return err
		}
	}

	log.Debugf(c, "feed: %s, putting %v entities", url, len(puts))
	if len(puts) > 1 {
		updateAverage(&f, f.Date, len(puts)-1)
		f.Date = time.Now()
		if !hasUpdated {
			f.Updated = f.Date
		}
	}
	scheduleNextUpdate(c, &f)
	if fromSub {
		wait := time.Now().Add(time.Hour * 6)
		if f.NextUpdate.Before(wait) {
			f.NextUpdate = wait
		}
	}
	delay := f.NextUpdate.Sub(time.Now())
	log.Infof(c, "feed: %s, next update scheduled for %v from now", url, delay-delay%time.Second)
	_, err = gn.PutMulti(puts)
	if err != nil {
		log.Errorf(c, "update put err: %v", err)
		return err
	}
	if generationChanged {
		return deleteOlderGeneration(c, gn, &f)
	}
	return nil
}

const keepGenerations = 5

func deleteOlderGeneration(c context.Context, gn *goon.Goon, f *Feed) error {
	if f.CurrGen <= keepGenerations {
		log.Infof(c, "feed: %s, ignoring remove, curr gen: %d ", f.Url, f.CurrGen)
		return nil
	}
	q := datastore.NewQuery(gn.Kind(&Story{})).Ancestor(gn.Key(f)).KeysOnly().
		FilterField("g", "<=", f.CurrGen-keepGenerations)
	keys, err := gn.GetAll(q, nil, true)
	if err != nil {
		return err
	}
	allKeys := make([]*datastore.Key, 0, len(keys)*2)
	scKind := gn.Kind(&StoryContent{})
	for _, key := range keys {
		allKeys = append(allKeys, key, datastore.IDKey(scKind, 1, key))
	}
	log.Infof(c, "feed: %s, removing count stories: %d, current gen: %d, ", f.Url, len(keys), f.CurrGen)
	err = gn.DeleteMulti(allKeys)
	if err != nil {
		log.Errorf(c, "feed: %s, delete old gen err: %v", f.Url, err)
	}
	return err
}

func UpdateFeed(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	c1, cf := context.WithTimeout(c, time.Minute)
	defer cf()
	gn := goon.FromContext(c1)
	url := r.FormValue("feed")
	if url == "" {
		log.Errorf(c, "empty update feed")
		return
	}
	log.Debugf(c, "update feed %s", url)
	last := len(r.FormValue("last")) > 0
	f := Feed{Url: url}
	s := ""
	if err := gn.Get(&f); errors.Is(err, datastore.ErrNoSuchEntity) {
		log.Errorf(c, "no such entity - "+url)
		s += "NSE"
		return
	} else if err != nil {
		s += "err - " + err.Error()
		return
	} else if last {
		// noop
	} else if time.Now().Before(f.NextUpdate) {
		log.Errorf(c, "feed %v already updated: %v", url, f.NextUpdate)
		s += "already updated"
		return
	}

	feedError := func(err error) {
		s += "feed err - " + err.Error()
		f.Error = err.Error()
		f.NextUpdate = time.Now().Add(time.Hour * 6)
		gn.Put(&f)
		log.Warningf(c, "error with %v, bump next update to %v, %v", url, f.NextUpdate, err)
	}

	if feed, stories, err := fetchFeed(c, f.Url, f.Url); err == nil {
		if err := updateFeed(c, f.Url, feed, stories, false, false, last); err != nil {
			feedError(err)
		} else {
			s += "success"
		}
	} else {
		feedError(err)
	}
}

func UpdateFeedLast(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	gn := goon.FromContext(c)
	url := r.FormValue("feed")
	log.Debugf(c, "update feed last %s", url)
	f := Feed{Url: url}
	if err := gn.Get(&f); err != nil {
		return
	}
	f.LastViewed = time.Now()
	gn.Put(&f)
}
