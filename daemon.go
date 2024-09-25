package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

func (s *syncer) daemonRun(ctx context.Context, slackSyncs []runSlackSync, runFreq time.Duration, slackDataUpdateCh <-chan dataUpdateResult) {
	runTicker := time.NewTicker(runFreq)
	defer runTicker.Stop()

	f := func() {
		err := s.RunOnce(ctx, slackSyncs)
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}

	f()
	for {
		select {
		case <-runTicker.C:
			f()
		case update := <-slackDataUpdateCh:
			if update.err != nil {
				fmt.Fprintf(os.Stderr, "Failed to update Slack data: %s\n", update.err)
			} else {
				fmt.Printf("Updating Slack data with %d user(s) and %d group(s)\n", len(update.slackUsers), len(update.slackUserGroups))

			}
		case <-ctx.Done():
			return
		}
	}
}

type slackDataUpdater struct {
	slClient        *slackMetaClient
	updateFrequency time.Duration
	updateCh        chan dataUpdateResult
}

type dataUpdateResult struct {
	slackUsers      slackUsers
	slackUserGroups UserGroups
	err             error
}

func newSlackDataUpdater(freq time.Duration, slClient *slackMetaClient) *slackDataUpdater {
	return &slackDataUpdater{
		slClient:        slClient,
		updateFrequency: freq,
		updateCh:        make(chan dataUpdateResult),
	}
}

func (u *slackDataUpdater) start(ctx context.Context) <-chan dataUpdateResult {
	resCh := make(chan dataUpdateResult, 1)
	ticker := time.NewTicker(u.updateFrequency)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				var (
					res dataUpdateResult
					err error
				)
				res.slackUsers, err = u.slClient.getSlackUsers(ctx)
				if err != nil {
					res.err = fmt.Errorf("failed to get Slack users: %s", err)
				} else {
					res.slackUserGroups, err = u.slClient.getUserGroups(ctx)
					if err != nil {
						res.err = fmt.Errorf("failed to get Slack user groups: %s", err)
					}
				}
				if ctx.Err() != nil {
					return
				}

				select {
				case resCh <- res:
				default:
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return resCh
}
