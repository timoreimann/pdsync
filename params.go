package main

import "time"

type params struct {
	config                string
	schedules             []string
	channelName           string
	channelID             string
	tmplString            string
	tmplFile              string
	dryRun                *bool
	daemon                bool
	daemonUpdateFrequency time.Duration
}
