package config

import (
	"context"
	"time"
)

type Session struct {
	ID      string
	Email   string
	Admin   bool
	Name    string
	Expires time.Time
}

type sessionKey struct{}

func GetSession(ctx context.Context) *Session {
	session, ok := ctx.Value(sessionKey{}).(*Session)
	if !ok {
		return nil
	}
	return session
}

func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}
