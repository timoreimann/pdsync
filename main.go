package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
)

var (
	pdToken                  string
	slToken                  string
	notAlphaNumRE            = regexp.MustCompile(`[^[:alnum:]]`)
	daemonMinUpdateFrequency = 1 * time.Minute
	daemonMaxExecutionTime   time.Duration
	includePrivateChannels   bool
)

func main() {
	var (
		p            params
		dryRun       bool
		pretendUsers bool
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
				EnvVars:     []string{"CONFIG_FILE"},
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
			&cli.DurationFlag{
				Name:        "daemon-max-execution-time",
				Usage:       "time after which the daemon should self-terminate (default: never)",
				Destination: &daemonMaxExecutionTime,
			},
			&cli.BoolFlag{
				Name:        "include-private-channels",
				Usage:       "update topics from rivate channels as well",
				Destination: &includePrivateChannels,
			},
			&cli.BoolFlag{
				Name:        "pretend-users",
				Usage:       "escape Slack user IDs to prevent tagging",
				Destination: &pretendUsers,
			},
			&cli.BoolFlag{
				Name:        "dry-run",
				Usage:       "do not update topic",
				Destination: &dryRun,
			},
			&cli.BoolFlag{
				Name:        "fail-fast",
				Usage:       "fail on the first schedule that cannot be synced, and otherwise handle failures gracefully (defaults to false when running in daemon mode, otherwise true)",
				Destination: &p.failFast,
			},
		},
		Action: func(c *cli.Context) error {
			p.schedules = c.StringSlice("schedule")
			if c.IsSet("pretend-users") {
				p.pretendUsers = &pretendUsers
			}
			if c.IsSet("dry-run") {
				p.dryRun = &dryRun
			}
			if !c.IsSet("fail-fast") {
				p.failFast = !p.daemon
			}
			return realMain(p)
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func realMain(p params) error {
	cfg, err := generateConfig(p)
	if err != nil {
		return err
	}

	if p.daemonUpdateFrequency < daemonMinUpdateFrequency {
		p.daemonUpdateFrequency = daemonMinUpdateFrequency
	}

	sp := syncerParams{
		pdClient: newPagerDutyClient(pdToken),
		slClient: newSlackMetaClient(slToken, includePrivateChannels),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

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

	fmt.Println("Getting Slack channels")
	slChannels, err := sp.slClient.getChannels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get channels: %s", err)
	}
	fmt.Printf("Got %d Slack channel(s)\n", len(slChannels))

	for _, cfgSlSync := range cfg.SlackSyncs {
		if err := cfgSlSync.populateChannel(ctx, slChannels); err != nil {
			return fmt.Errorf("failed to populate channel %s", err)
		}

		if err := cfgSlSync.populateTemplate(ctx); err != nil {
			return fmt.Errorf("failed to populate templates: %s", err)
		}
	}

	slSyncs, err := sp.createSlackSyncs(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create Slack syncs: %s", err)
	}

	err = cfg.validateConfig()
	if err != nil {
		return fmt.Errorf("failed to validate config: %s", err)
	}

	syncer := newSyncer(sp)

	runFunc := func() error {
		return syncer.Run(ctx, slSyncs, p.failFast)
	}
	if !p.daemon {
		return runFunc()
	}

	daemonCtx := ctx
	if daemonMaxExecutionTime > 0 {
		fmt.Printf("Setting maximum daemon execution time to %s\n", daemonMaxExecutionTime)
		var cancel context.CancelFunc
		daemonCtx, cancel = context.WithTimeout(ctx, daemonMaxExecutionTime)
		defer cancel()
	}

	fmt.Println("Starting daemon")
	startDaemon(daemonCtx, p.daemonUpdateFrequency, runFunc)

	termMessage := "Daemon terminated"
	if daemonCtx.Err() != nil && ctx.Err() == nil {
		termMessage += " (maximum execution time has elapsed)"
	}
	fmt.Println(termMessage)
	return nil
}
