package main

import (
	"fmt"
	"time"
)

func withRetry(maxAttempts int, fn func() error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var last error
	for i := 1; i <= maxAttempts; i++ {
		if err := fn(); err != nil {
			last = err
			if !isRetryableExecErr(err) || i == maxAttempts {
				break
			}
			time.Sleep(time.Duration(1<<(i-1)) * 200 * time.Millisecond)
			continue
		}
		return nil
	}
	if last == nil {
		return nil
	}
	return fmt.Errorf("request failed after %d attempts: %w", maxAttempts, last)
}
