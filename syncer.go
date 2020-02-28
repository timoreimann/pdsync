package main

import (
	"context"
	"text/template"

	"bytes"
	"fmt"

	"github.com/slack-go/slack"
)

type syncerParams struct {
	pdClient *pagerDutyClient
	slClient *slackClient
	tmpl     *template.Template
	dryRun   bool
}

type syncer struct {
	syncerParams
}

func newSyncer(p syncerParams) *syncer {
	return &syncer{
		syncerParams: p,
	}
}

func (s *syncer) Run(ctx context.Context, schedules pdSchedules, channel *slack.Channel, slUsers slackUsers) error {
	fmt.Println("Getting PageDuty on-call users by schedule")
	onCallUsersBySchedule, err := s.pdClient.getOnCallUsersBySchedule(schedules)
	if err != nil {
		return err
	}

	slackUserIDByScheduleName := map[string]string{}
	for schedule, onCallUser := range onCallUsersBySchedule {
		slUser := slUsers.findByPDUser(onCallUser)
		if slUser == nil {
			fmt.Printf("Failed to find Slack user for PD user %s\n", pagerDutyUserString(onCallUser))
			continue
		}

		cleanScheduleName := notAlphaNumRE.ReplaceAllString(schedule.name, "")
		slackUserIDByScheduleName[cleanScheduleName] = slUser.id
	}

	var buf bytes.Buffer
	err = s.tmpl.Execute(&buf, slackUserIDByScheduleName)
	if err != nil {
		return err
	}

	topic := buf.String()
	err = s.slClient.ensureTopic(ctx, channel, topic, s.dryRun)
	if err != nil {
		return err
	}

	return nil
}
