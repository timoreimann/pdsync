package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

func daemonRun(ctx context.Context, freq time.Duration, s *syncer, f func() error) {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	errLogF := func() {
		err := f()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}

	errLogF()
	for {
		select {
		case <-ticker.C:
			errLogF()
		case <-ctx.Done():
			return
		}
	}
}
