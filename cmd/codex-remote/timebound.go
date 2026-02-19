package main

import (
	"errors"
	"strings"
	"time"
)

func normalizeTimeBound(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC().Format(time.RFC3339Nano), nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return "", errors.New("must be RFC3339 or duration like 10m/2h")
	}
	return time.Now().UTC().Add(-d).Format(time.RFC3339Nano), nil
}
