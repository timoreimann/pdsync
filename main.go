package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
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
	var (
		p      params
		dryRun bool
	)
	app := &cli.App{
		Name:  "pdsync",
		Usage: "sync PagerDuty on-call schedules to Slack",
		UsageText: `Poll a list of PagerDuty schedules for on-call personnel and update a Slack channel's topic using a predefined template

Schedules can be given as names or IDs. Similarly, the channel to update the topic for can be specified by name or ID.

Optionally, a set of Slack user groups can be kept in sync. This can be used to manage on-call handles.

By default, the program will terminate after a single run. Use the --daemon flag to keep running in the background and synchronize schedule changes periodically.
`,
		Authors: []*cli.Author{
			{
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
			&cli.StringFlag{
				Name:        "config",
				Usage:       "config file to use",
				Destination: &p.config,
			},
			&cli.StringSliceFlag{
				Name:  "schedule",
				Usage: "name of a PageDuty schedule to sync periodically (can be repeated to define several schedules); syntax: id|name=<schedule reference>[;userGroup=id|name|handle=<user group reference>..]",
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
				Destination: &dryRun,
			},
		},
		Action: func(c *cli.Context) error {
			p.schedules = c.StringSlice("schedule")
			if c.IsSet("dry-run") {
				p.dryRun = &dryRun
			}
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
	cfg, err := generateConfig(p)
	if err != nil {
		return err
	}

	if p.daemonUpdateFrequency < minDaemonUpdateFrequency {
		p.daemonUpdateFrequency = minDaemonUpdateFrequency
	}

	sp := syncerParams{
		pdClient: newPagerDutyClient(pdToken),
		slClient: newSlackMetaClient(slToken),
	}

	ctx := context.Background()

	fmt.Println("Getting Slack users")
	sp.slackUsers, err = sp.slClient.getSlackUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Slack users: %s", err)
	}
	fmt.Printf("Found %d Slack user(s)\n", len(sp.slackUsers))

	fmt.Println("Getting Slack user groups")
	sp.slackUserGroups, err = sp.slClient.getUserGroups(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Slack user groups: %s", err)
	}
	fmt.Printf("Found %d Slack user group(s)\n", len(sp.slackUserGroups))

	slSyncs, err := sp.createSlackSyncs(context.TODO(), cfg)
	if err != nil {
		return err
	}

	syncer := newSyncer(sp)

	runFunc := func() error {
		return syncer.Run(ctx, slSyncs)
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
