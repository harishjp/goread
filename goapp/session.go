package goread

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/google/uuid"
	"github.com/harishjp/goread/config"
	"github.com/harishjp/goread/goon"
	"google.golang.org/api/idtoken"
)

type SessionHandler struct {
	sessions map[string]*config.Session
}

func NewSessionHandler() *SessionHandler {
	return &SessionHandler{
		sessions: make(map[string]*config.Session),
	}
}

const sessionCookieName = "s"

func (h *SessionHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		email := ""
		if !errors.Is(err, http.ErrNoCookie) {
			session := h.sessions[cookie.Value]
			if session != nil && session.Expires.After(time.Now()) {
				email = session.Email
				r = r.WithContext(config.WithSession(r.Context(), h.sessions[cookie.Value]))
			}
		}
		path := r.URL.Path
		if strings.HasPrefix(path, "/admin/") && (email == "" || email != config.AdminEmail()) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(path, "/user/") && email == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(path, "/tasks/") && r.Header.Get("X-Appengine-Taskname") == "" {
			http.Error(w, "Bad Request - Invalid Task", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *SessionHandler) createSession(payload *idtoken.Payload, w http.ResponseWriter) error {
	sessionUUID, err := uuid.NewUUID()
	if err != nil {
		return fmt.Errorf("failed to create uuid: %w", err)
	}

	sessionId := sessionUUID.String()
	expiryTime := time.Now().AddDate(0, 0, 7)
	email := payload.Claims["email"].(string)
	h.sessions[sessionId] = &config.Session{
		ID:      payload.Subject,
		Name:    payload.Claims["name"].(string),
		Email:   email,
		Admin:   email == config.AdminEmail(),
		Expires: expiryTime,
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionId,
		Path:     "/",
		Expires:  expiryTime,
		Secure:   !config.IsDevServer(),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

const googleCsrfToken = "g_csrf_token"

func verfiyToken(r *http.Request) (bool, string) {
	formToken := r.FormValue(googleCsrfToken)
	if formToken == "" {
		return false, "form token required"
	}
	cookieToken, err := r.Cookie(googleCsrfToken)
	if err != nil {
		return false, "cookie not found"
	}
	if formToken != cookieToken.Value {
		return false, "tokens not equal"
	}
	return true, ""
}

func getPayload(r *http.Request) (*idtoken.Payload, error) {
	validator, err := idtoken.NewValidator(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}
	credential := r.FormValue("credential")
	return validator.Validate(r.Context(), credential, config.ClientID())
}

func (h *SessionHandler) Login(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()

	if ok, msg := verfiyToken(r); !ok {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	data, err := getPayload(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("error getting payload: %v", err), http.StatusBadRequest)
		return
	}

	if !data.Claims["email_verified"].(bool) {
		http.Error(w, "email not verified", http.StatusForbidden)
		return
	}
	u := &User{
		Id: data.Subject,
	}
	gn := goon.FromContext(r.Context())
	if err = gn.Get(u); errors.Is(err, datastore.ErrNoSuchEntity) {
		u.Email = data.Claims["email"].(string)
		u.Read = time.Now().Add(-time.Hour * 24)
		gn.Put(u)
	}
	if err = h.createSession(data, w); err != nil {
		http.Error(w, fmt.Sprintf("error creating session: %v", err), http.StatusInternalServerError)
	} else {
		w.Header().Add("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><head><meta http-equiv=\"refresh\" content=\"0; url=/\"></head></html>"))
		// Server side redirect does not work, the browser will not set the cookie because chained redirects use
		// original referrer.
		// http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

func (h *SessionHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionCookie, err := r.Cookie(sessionCookieName)
	if !errors.Is(err, http.ErrNoCookie) {
		sessionCookie.Expires = time.Now().Add(-time.Hour)
		http.SetCookie(w, sessionCookie)
		delete(h.sessions, sessionCookie.Value)
	}
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}
