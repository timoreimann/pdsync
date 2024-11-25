package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"
)

type runSlackSync struct {
	name           string
	pdSchedules    pdSchedules
	slackChannelID string
	topicTemplate  *template.Template
	dryRun         bool
	pretendUsers   bool
	slChannels     *channelList
}

type syncerParams struct {
	pdClient        *pagerDutyClient
	slClient        *slackMetaClient
	slackUsers      slackUsers
	slackUserGroups UserGroups
}

func (sp syncerParams) createSlackSyncs(ctx context.Context, cfg config) ([]runSlackSync, error) {
	var slSyncs []runSlackSync

	for _, cfgSlSync := range cfg.SlackSyncs {
		slSync := runSlackSync{
			name:         cfgSlSync.Name,
			pretendUsers: cfgSlSync.PretendUsers,
			dryRun:       cfgSlSync.DryRun,
		}

		if cfgSlSync.Template == nil {
			fmt.Printf("Slack sync %s: skipping topic handling because template is undefined\n", slSync.name)
		}

		pdSchedules := pdSchedules{}
		fmt.Printf("Slack sync %s: Getting PagerDuty schedules\n", slSync.name)
		for _, schedule := range cfgSlSync.Schedules {
			pdSchedule, err := sp.pdClient.getSchedule(ctx, schedule.ID, schedule.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to create slack sync %q: failed to get schedule %s: %s", slSync.name, schedule, err)
			}

			if pdSchedule == nil {
				return nil, fmt.Errorf("failed to create slack sync %q: schedule %s not found", slSync.name, schedule)
			}

			for _, cfgUserGroup := range schedule.UserGroups {
				ug := sp.slackUserGroups.find(cfgUserGroup)
				if ug == nil {
					return nil, fmt.Errorf("failed to create slack sync %q: user group %s not found", slSync.name, cfgUserGroup)
				}
				fmt.Printf("Slack sync %s: assigning user group %s to schedule %s\n", slSync.name, ug, pdSchedule)
				pdSchedule.userGroups = append(pdSchedule.userGroups, *ug)
			}

			pdSchedules.ensureSchedule(*pdSchedule)

			for _, cfgUserGroup := range schedule.UserGroups {
				ug := sp.slackUserGroups.find(cfgUserGroup)
				if ug == nil {
					return nil, fmt.Errorf("failed to create slack sync %q: user group %s not found", slSync.name, ug)
				}
			}
		}
		slSync.pdSchedules = pdSchedules
		fmt.Printf("Slack sync %s: found %d PagerDuty schedule(s)\n", slSync.name, len(pdSchedules))

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

func (s *syncer) Run(ctx context.Context, slackSyncs []runSlackSync, failFast bool) error {
	for _, slackSync := range slackSyncs {
		err := s.runSlackSync(ctx, slackSync)
		if err != nil {
			msg := fmt.Sprintf("failed to run Slack sync %s: %s", slackSync.name, err)
			if failFast || ctx.Err() != nil {
				return errors.New(msg)
			}

			formattedMsg := strings.ToUpper(string(msg[0])) + msg[1:]
			fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
		}
	}

	return nil
}

func (s *syncer) joinChannel(ctx context.Context, slackSync runSlackSync) error {
	if slackSync.dryRun {
		return nil
	}

	if len(slackSync.slackChannelID) == 0 {
		fmt.Printf("no channel for %s slack sync, so skip joining", slackSync.name)
		return nil
	}

	joined, err := s.slClient.joinChannel(ctx, slackSync.slackChannelID)
	if err != nil {
		if strings.Contains(err.Error(), "missing_scope") {
			fmt.Printf(`cannot automatically join channel with ID %s because of missing scope "channels:join" -- please add the scope or join pdsync manually`, slackSync.slackChannelID)
		} else {
			return fmt.Errorf("failed to join channel with ID %s: %s", slackSync.slackChannelID, err)
		}
	}
	if joined {
		fmt.Printf("joined channel with ID %s\n", slackSync.slackChannelID)
	}

	return nil
}

func (s *syncer) updateTopic(ctx context.Context, slackSync runSlackSync, slackUserIDByScheduleName map[string]string) error {
	if slackSync.dryRun {
		return nil
	}

	if slackSync.topicTemplate == nil {
		fmt.Println("Skipping topic update")
		return nil
	}

	var buf bytes.Buffer
	fmt.Printf("Executing template with Slack user IDs by schedule name: %s\n", slackUserIDByScheduleName)
	err := slackSync.topicTemplate.Execute(&buf, slackUserIDByScheduleName)
	if err != nil {
		return fmt.Errorf("failed to render template: %s", err)
	}

	topic := buf.String()
	err = s.slClient.updateTopic(ctx, slackSync.slackChannelID, topic, slackSync.dryRun)
	if err != nil {
		return fmt.Errorf("failed to update topic: %s", err)
	}
	return nil
}

func (s *syncer) runSlackSync(ctx context.Context, slackSync runSlackSync) error {
	s.joinChannel(ctx, slackSync)

	ocgs := oncallGroups{}
	slackUserIDByScheduleName := map[string]string{}
	for _, schedule := range slackSync.pdSchedules {
		fmt.Printf("Processing schedule %s\n", schedule)
		onCallUser, err := s.pdClient.getOnCallUser(ctx, schedule)
		if err != nil {
			return fmt.Errorf("failed to get on call user for schedule %q: %s", schedule.name, err)
		}

		slUser := s.slackUsers.findByPDUser(onCallUser)
		if slUser == nil {
			return fmt.Errorf("failed to find Slack user for PD user %s", pagerDutyUserString(onCallUser))
		}

		for _, userGroup := range schedule.userGroups {
			fmt.Printf("Ensuring member %s for user group %s\n", slUser.id, userGroup)
			ocgs.getOrCreate(userGroup).ensureMember(slUser.id)
		}

		slUserID := slUser.id
		if slackSync.pretendUsers {
			slUserID = fmt.Sprintf(`\%s`, slUserID)
		}

		cleanScheduleName := notAlphaNumRE.ReplaceAllString(schedule.name, "")
		slackUserIDByScheduleName[cleanScheduleName] = slUserID
	}

	if err := s.slClient.updateOncallGroupMembers(ctx, ocgs, slackSync.dryRun); err != nil {
		return fmt.Errorf("failed to update on-call user group members: %s", err)
	}

	if err := s.updateTopic(ctx, slackSync, slackUserIDByScheduleName); err != nil {
		return fmt.Errorf("failed to channel template: %s", err)
	}

	return nil
}
