package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/urfave/cli/v2"
)

var (
	pdToken                  string
	slToken                  string
	notAlphaNumRE            = regexp.MustCompile(`[^[:alnum:]]`)
	minDaemonUpdateFrequency = 1 * time.Minute
)

func main() {
	var p params
	app := &cli.App{
		Name:  "pdsync",
		Usage: "sync PagerDuty on-call schedules to Slack",
		UsageText: `Poll a list of PagerDuty schedules for on-call personnel and update a Slack channel's topic using a predefined template

Schedules can be given as names or IDs. Similarly, the channel to update the topic for can be specified by name or ID.

By default, the program will terminate after a single run. Use the --daemon flag to keep running in the background and synchronize schedule changes periodically.
`,
		Authors: []*cli.Author{
			&cli.Author{
				Name:  "Timo Reimann",
				Email: "ttr314@googlemail.com",
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "pagerduty-token",
				Usage:       "the PagerDuty token",
				Destination: &pdToken,
				EnvVars:     []string{"PAGERDUTY_TOKEN"},
				Required:    true,
			},
			&cli.StringFlag{
				Name:        "slack-token",
				Usage:       "the Slack token",
				Destination: &slToken,
				EnvVars:     []string{"SLACK_TOKEN"},
				Required:    true,
			},
			&cli.StringSliceFlag{
				Name:  "schedule-names",
				Usage: "names of PageDuty schedules to sync periodically",
			},
			&cli.StringSliceFlag{
				Name:  "schedule-ids",
				Usage: "IDs of PageDuty schedules to sync periodically",
			},
			&cli.StringFlag{
				Name:        "channel-name",
				Usage:       "the name of the channel to post topic updates to",
				Destination: &p.channelName,
			},
			&cli.StringFlag{
				Name:        "channel-id",
				Usage:       "the ID of the channel to post topic updates to",
				Destination: &p.channelID,
			},
			&cli.StringFlag{
				Name:        "template",
				Usage:       "the literal Go template for the channel topic to apply; variable like {{.<ScheduleName>}} are replaced by the on-caller's Slack handle",
				Destination: &p.tmplString,
			},
			&cli.StringFlag{
				Name:        "template-file",
				Usage:       "a template file `FILE` describing the Go template for the channel topic to apply",
				Destination: &p.tmplFile,
			},
			&cli.BoolFlag{
				Name:        "daemon",
				Usage:       "run as daemon in the background and update periodically",
				Destination: &p.daemon,
			},
			&cli.DurationFlag{
				Name:        "daemon-update-frequency",
				Value:       5 * time.Minute,
				Usage:       "how often on-call schedules should be checked for changes (minimum is 1 minute)",
				Destination: &p.daemonUpdateFrequency,
			},
			&cli.BoolFlag{
				Name:        "dry-run",
				Usage:       "do not update topic",
				Destination: &p.dryRun,
			},
		},
		Action: func(c *cli.Context) error {
			p.scheduleNames = c.StringSlice("schedule-names")
			p.scheduleIDs = c.StringSlice("schedule-ids")
			return realMain(p)
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		// fmt.Fprintf(os.Stderr, "%+#v\n", err)
		os.Exit(1)
	}
}

func realMain(p params) error {
	if len(p.scheduleNames) == 0 && len(p.scheduleIDs) == 0 {
		return fmt.Errorf("one of --schedule-names or --schedule-ids must be provided")
	}

	if p.channelName == "" && p.channelID == "" {
		return fmt.Errorf("one of --channel-name or --channel-id must be provided")
	}

	if p.tmplString == "" && p.tmplFile == "" {
		return fmt.Errorf("one of --template or --template-file must be provided")
	}

	if p.tmplFile != "" {
		b, err := ioutil.ReadFile(p.tmplFile)
		if err != nil {
			return err
		}
		p.tmplString = string(b)
	}

	if p.daemonUpdateFrequency < minDaemonUpdateFrequency {
		p.daemonUpdateFrequency = minDaemonUpdateFrequency
	}

	sp := syncerParams{
		pdClient: newPagerDutyClient(pdToken),
		slClient: newSlackClient(slToken),
		dryRun:   p.dryRun,
	}
	var err error
	sp.tmpl, err = template.New("topic").Parse(p.tmplString)
	if err != nil {
		return fmt.Errorf("failed to parse template %q: %s", p.tmplString, err)
	}

	syncer := newSyncer(sp)

	ctx := context.Background()

	slChannel, err := sp.slClient.getChannel(ctx, p.channelName, p.channelID)
	if err != nil {
		return fmt.Errorf("failed to get Slack channel: %s", err)
	}
	fmt.Printf("Found channel %q (ID %s)\n", slChannel.Name, slChannel.ID)

	fmt.Println("Getting Slack users")
	slUsers, err := sp.slClient.getSlackUsers(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Found %d user(s)\n", len(slUsers))

	fmt.Println("Getting PagerDuty schedules")
	pdSchedules, err := sp.pdClient.getSchedules(p.scheduleIDs, p.scheduleNames)
	if err != nil {
		return fmt.Errorf("failed to get PagerDuty schedules: %s", err)
	}
	fmt.Printf("Found %d PagerDuty schedule(s)\n", len(pdSchedules))

	runFunc := func() error {
		return syncer.Run(ctx, pdSchedules, slChannel, slUsers)
	}
	if !p.daemon {
		return runFunc()
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	stopCtx, cancel := context.WithCancel(context.Background())

	go func() {
		<-c
		cancel()
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("Starting daemon")
		daemonRun(stopCtx, p.daemonUpdateFrequency, syncer, runFunc)
	}()

	wg.Wait()
	return nil
}
