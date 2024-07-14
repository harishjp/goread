// Copyright 2024 harishjp

package log

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
)

type Entry struct {
	Message  string `json:"message"`
	Severity string `json:"severity,omitempty"`
	Trace    string `json:"logging.googleapis.com/trace,omitempty"`

	// Logs Explorer allows filtering and display of this as `jsonPayload.component`.
	Component string `json:"component,omitempty"`
}

func logf(_ context.Context, severity string, format string, args ...interface{}) {
	entry := Entry{
		Message:   fmt.Sprintf(format, args...),
		Severity:  severity,
		Trace:     "", // Populate using trace header: 'X-Cloud-Trace-Context'
		Component: "",
	}

	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("Failed to marshal entry: %v", err)
	} else {
		log.Println(string(b))
	}
}

func Debugf(ctx context.Context, format string, args ...interface{}) {
	logf(ctx, "DEBUG", format, args...)
}

func Infof(ctx context.Context, format string, args ...interface{}) {
	logf(ctx, "INFO", format, args...)
}

func Warningf(ctx context.Context, format string, args ...interface{}) {
	logf(ctx, "WARNING", format, args...)
}

func Errorf(ctx context.Context, format string, args ...interface{}) {
	logf(ctx, "ERROR", format, args...)
}

func Criticalf(ctx context.Context, format string, args ...interface{}) {
	logf(ctx, "CRITICAL", format, args...)
}

func Fatal(err error) {
	Criticalf(context.Background(), "error occured: %v", err)
	os.Exit(1)
}
