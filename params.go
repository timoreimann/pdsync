package main

import "time"

type params struct {
	scheduleNames         []string
	scheduleIDs           []string
	channelName           string
	channelID             string
	tmplString            string
	tmplFile              string
	daemon                bool
	daemonUpdateFrequency time.Duration
	dryRun                bool
}
