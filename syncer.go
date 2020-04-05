package main

import (
	"context"
	"os"
	"text/template"

	"bytes"
	"fmt"
)

type runSlackSync struct {
	name           string
	pdSchedules    pdSchedules
	slackChannelID string
	tmpl           *template.Template
	dryRun         bool
}

type syncerParams struct {
	pdClient   *pagerDutyClient
	slClient   *slackClient
	slackUsers slackUsers
}

func (sp syncerParams) createSlackSyncs(ctx context.Context, cfg config) ([]runSlackSync, error) {
	var slSyncs []runSlackSync
	for _, cfgSlSync := range cfg.SlackSyncs {
		slSync := runSlackSync{
			name:   cfgSlSync.Name,
			dryRun: cfgSlSync.DryRun,
		}

		slChannel, err := sp.slClient.getChannel(ctx, cfgSlSync.Channel.Name, cfgSlSync.Channel.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to create slack sync %q: failed to get Slack channel: %s", slSync.name, err)
		}
		slSync.slackChannelID = slChannel.ID
		fmt.Printf("Slack sync %s: found Slack channel %q (ID %s)\n", slSync.name, slChannel.Name, slChannel.ID)

		var pdSchedules pdSchedules
		fmt.Printf("Slack sync %s: Getting PagerDuty schedules\n", slSync.name)
		for _, schedule := range cfgSlSync.Schedules {
			pdSchedule, err := sp.pdClient.getSchedule(schedule.ID, schedule.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to create slack sync %q: failed to get schedule %#v: %s", slSync.name, schedule, err)
			}

			if pdSchedule == nil {
				return nil, fmt.Errorf("failed to create slack sync %q: schedule %#v not found", slSync.name, schedule)
			}
			pdSchedules.ensureSchedule(*pdSchedule)
		}
		slSync.pdSchedules = pdSchedules
		fmt.Printf("Slack sync %s: found %d PagerDuty schedule(s)\n", slSync.name, len(pdSchedules))

		slSync.tmpl, err = template.New("topic").Parse(cfgSlSync.Template)
		if err != nil {
			return nil, fmt.Errorf("failed to create slack sync %q: failed to parse template %q: %s", slSync.name, cfgSlSync.Template, err)
		}

		slSyncs = append(slSyncs, slSync)
	}

	return slSyncs, nil
}

type syncer struct {
	syncerParams
}

func newSyncer(sp syncerParams) *syncer {
	return &syncer{
		syncerParams: sp,
	}
}

func (s *syncer) Run(ctx context.Context, slackSyncs []runSlackSync) error {
	for _, slackSync := range slackSyncs {
		err := s.runSlackSync(ctx, slackSync)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to run Slack sync %s: %s", slackSync.name, err)
			// TODO: aggregate and return errors
			continue
		}
	}

	return nil
}

func (s *syncer) runSlackSync(ctx context.Context, slackSync runSlackSync) error {
	onCallUsersBySchedule, err := s.pdClient.getOnCallUsersBySchedule(slackSync.pdSchedules)
	if err != nil {
		return fmt.Errorf("failed to get on call users by schedule: %s", err)
	}

	slackUserIDByScheduleName := map[string]string{}
	for schedule, onCallUser := range onCallUsersBySchedule {
		slUser := s.slackUsers.findByPDUser(onCallUser)
		if slUser == nil {
			return fmt.Errorf("failed to find Slack user for PD user %s", pagerDutyUserString(onCallUser))
		}

		cleanScheduleName := notAlphaNumRE.ReplaceAllString(schedule.name, "")
		slackUserIDByScheduleName[cleanScheduleName] = slUser.id
	}

	var buf bytes.Buffer
	fmt.Printf("Executing template with Slack user IDs by schedule name: %#v\n", slackUserIDByScheduleName)
	err = slackSync.tmpl.Execute(&buf, slackUserIDByScheduleName)
	if err != nil {
		return fmt.Errorf("failed to render template: %s", err)
	}

	topic := buf.String()
	err = s.slClient.ensureTopic(ctx, slackSync.slackChannelID, topic, slackSync.dryRun)
	if err != nil {
		return fmt.Errorf("failed to ensure topic: %s", err)
	}

	return nil
}
