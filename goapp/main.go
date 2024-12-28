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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/gorilla/mux"
	"github.com/harishjp/goread/config"
	"github.com/harishjp/goread/goon"
	"github.com/harishjp/goread/log"
	"github.com/harishjp/goread/miniprofiler"
	mpg "github.com/harishjp/goread/miniprofiler_gae"
)

var (
	templates   *template.Template
	mobileIndex []byte
)

func Init(router *mux.Router) {
	var err error
	if templates, err = template.New("").Funcs(funcs).
		ParseFiles(
			"app/templates/base.html",
			"app/templates/admin-all-feeds.html",
			"app/templates/admin-date-formats.html",
			"app/templates/admin-feed.html",
			"app/templates/admin-stats.html",
			"app/templates/admin-user.html",
		); err != nil {
		log.Fatal(err)
	}
	mobileIndex, err = os.ReadFile("app/static/index.html")
	if err != nil {
		log.Fatal(err)
	}

	miniprofiler.ToggleShortcut = "Alt+C"
	miniprofiler.Position = "bottomleft"

	RegisterDatastoreClient(router)
	RegisterHandlers(router)
	getURL = func(name string, pairs ...string) (*url.URL, error) {
		return router.Get(name).URL(pairs...)
	}
}

func RegisterDatastoreClient(router *mux.Router) {
	client, err := datastore.NewClient(context.Background(), datastore.DetectProjectID)
	if err != nil {
		log.Fatal(err)
	}
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(goon.ContextWithClient(r.Context(), client)))
		})
	})
}

func RegisterHandlers(router *mux.Router) {
	router.Use(mpg.Middleware)
	sessionHandler := NewSessionHandler()
	router.Use(sessionHandler.Middleware)

	router.HandleFunc("/", Main).Name("main")
	router.HandleFunc("/login/callback", sessionHandler.Login).Name("callback-google")
	router.HandleFunc("/login/redirect", LoginRedirect)
	router.HandleFunc("/logout", sessionHandler.Logout).Name("logout")
	router.HandleFunc("/push", SubscribeCallback).Name("subscribe-callback")
	router.HandleFunc("/tasks/import-opml", ImportOpmlTask).Name("import-opml-task")
	router.HandleFunc("/tasks/subscribe-feed", SubscribeFeed).Name("subscribe-feed")
	router.HandleFunc("/tasks/update-feed-last", UpdateFeedLast).Name("update-feed-last")
	router.HandleFunc("/tasks/update-feed-manual", UpdateFeed).Name("update-feed-manual")
	router.HandleFunc("/tasks/update-feed", UpdateFeed).Name("update-feed")
	router.HandleFunc("/tasks/update-feeds", UpdateFeeds).Name("update-feeds")
	router.HandleFunc("/tasks/delete-old-feeds", DeleteOldFeeds).Name("delete-old-feeds")
	router.HandleFunc("/tasks/delete-old-feed", DeleteOldFeed).Name("delete-old-feed")

	router.Handle("/user/add-subscription", wrap(AddSubscription)).Name("add-subscription")
	router.Handle("/user/delete-account", wrap(DeleteAccount)).Name("delete-account")
	router.Handle("/user/export-opml", wrap(ExportOpml)).Name("export-opml")
	router.Handle("/user/feed-history", wrap(FeedHistory)).Name("feed-history")
	router.Handle("/user/get-contents", wrap(GetContents)).Name("get-contents")
	router.Handle("/user/get-feed", wrap(GetFeed)).Name("get-feed")
	router.Handle("/user/get-stars", wrap(GetStars)).Name("get-stars")
	router.Handle("/user/import/opml", wrap(ImportOpml)).Name("import-opml")
	router.Handle("/user/list-feeds", wrap(ListFeeds)).Name("list-feeds")
	router.Handle("/user/mark-read", wrap(MarkRead)).Name("mark-read")
	router.Handle("/user/mark-unread", wrap(MarkUnread)).Name("mark-unread")
	router.Handle("/user/save-options", wrap(SaveOptions)).Name("save-options")
	router.Handle("/user/set-star", wrap(SetStar)).Name("set-star")
	router.Handle("/user/upload-opml", wrap(UploadOpml)).Name("upload-opml")

	router.HandleFunc("/admin/all-feeds", AllFeeds).Name("all-feeds")
	router.HandleFunc("/admin/all-feeds-opml", AllFeedsOpml).Name("all-feeds-opml")
	router.HandleFunc("/admin/user", AdminUser).Name("admin-user")
	router.HandleFunc("/date-formats", AdminDateFormats).Name("admin-date-formats")
	router.HandleFunc("/admin/feed", AdminFeed).Name("admin-feed")
	router.HandleFunc("/admin/subhub", AdminSubHub).Name("admin-subhub-feed")
	router.HandleFunc("/admin/stats", AdminStats).Name("admin-stats")
	router.HandleFunc("/admin/update-feed", AdminUpdateFeed).Name("admin-update-feed")
	router.HandleFunc("/user/charge", Charge).Name("charge")
	router.HandleFunc("/user/account", Account).Name("account")
	router.HandleFunc("/user/uncheckout", Uncheckout).Name("uncheckout")

	//router.Handle("/tasks/delete-blobs", mpg.NewHandler(DeleteBlobs)).Name("delete-blobs")

	if !config.IsDevServer() {
		log.Infof(context.Background(), "Production server not adding statics")
		return
	}

	router.PathPrefix("/static/").Handler(http.FileServer(http.Dir("./app/")))
	router.HandleFunc("/user/clear-feeds", ClearFeeds).Name("clear-feeds")
	router.HandleFunc("/user/clear-read", ClearRead).Name("clear-read")
	router.HandleFunc("/test/atom.xml", TestAtom).Name("test-atom")
}

func wrap(f http.HandlerFunc) http.HandlerFunc {
	if config.IsDevServer() {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Add("Access-Control-Allow-Credentials", "true")
			f(w, r)
		}
	}
	return f
}

func Main(w http.ResponseWriter, r *http.Request) {
	c := r.Context()
	ua := r.Header.Get("User-Agent")
	mobile := strings.Contains(ua, "Mobi")
	if desktop, _ := r.Cookie("goread-desktop"); desktop != nil {
		switch desktop.Value {
		case "desktop":
			mobile = false
		case "mobile":
			mobile = true
		}
	}
	if mobile {
		_, err := w.Write(mobileIndex)
		if err != nil {
			log.Errorf(c, "error writing mobile index: %v", err)
		}
	} else {
		if err := templates.ExecuteTemplate(w, "base.html", includes(c, w, r)); err != nil {
			log.Errorf(c, "error executing template: %v", err)
			serveError(w, err)
		}
	}
}

func addFeed(c context.Context, userid string, outline *OpmlOutline) error {
	gn := goon.FromContext(c)
	o := outline.Outline[0]
	log.Infof(c, "adding feed %v to user %s", o.XmlUrl, userid)
	fu, ferr := url.Parse(o.XmlUrl)
	if ferr != nil {
		return ferr
	}
	fu.Fragment = ""
	o.XmlUrl = fu.String()

	f := Feed{Url: o.XmlUrl}
	if err := gn.Get(&f); errors.Is(err, datastore.ErrNoSuchEntity) {
		if feed, stories, err := fetchFeed(c, o.XmlUrl, o.XmlUrl); err != nil {
			return fmt.Errorf("could not add feed %s: %v", o.XmlUrl, err)
		} else {
			f = *feed
			f.Updated = time.Time{}
			f.Checked = f.Updated
			f.NextUpdate = f.Updated
			f.LastViewed = time.Now()
			if _, err := gn.Put(&f); err != nil {
				log.Warningf(c, "could not add feed %s: %v", o.XmlUrl, err)
			}
			for _, s := range stories {
				s.Created = s.Published
			}
			if err := updateFeed(c, f.Url, feed, stories, false, false, false); err != nil {
				return err
			}

			o.XmlUrl = feed.Url
			o.HtmlUrl = feed.Link
			if o.Title == "" {
				o.Title = feed.Title
			}
		}
	} else if err != nil {
		return err
	} else {
		o.HtmlUrl = f.Link
		if o.Title == "" {
			o.Title = f.Title
		}
	}
	o.Text = ""

	return nil
}

func mergeUserOpml(_ context.Context, ud *UserData, outlines ...*OpmlOutline) error {
	var fs Opml
	if len(ud.Opml) > 0 {
		if err := json.Unmarshal(ud.Opml, &fs); err != nil {
			return fmt.Errorf("error reading user %s opml: %w", ud.Id, err)
		}
	}
	urls := make(map[string]bool)

	for _, o := range fs.Outline {
		if o.XmlUrl != "" {
			urls[o.XmlUrl] = true
		} else {
			for _, so := range o.Outline {
				urls[so.XmlUrl] = true
			}
		}
	}

	mergeOutline := func(label string, outline *OpmlOutline) {
		if _, present := urls[outline.XmlUrl]; present {
			return
		} else {
			urls[outline.XmlUrl] = true

			if label == "" {
				fs.Outline = append(fs.Outline, outline)
			} else {
				done := false
				for _, ol := range fs.Outline {
					if ol.Title == label && ol.XmlUrl == "" {
						ol.Outline = append(ol.Outline, outline)
						done = true
						break
					}
				}
				if !done {
					fs.Outline = append(fs.Outline, &OpmlOutline{
						Title:   label,
						Outline: []*OpmlOutline{outline},
					})
				}
			}
		}
	}

	for _, outline := range outlines {
		if outline.XmlUrl != "" {
			mergeOutline("", outline)
		} else {
			for _, o := range outline.Outline {
				mergeOutline(outline.Title, o)
			}
		}
	}

	b, err := json.Marshal(&fs)
	if err != nil {
		return err
	}
	ud.Opml = b
	return nil
}
