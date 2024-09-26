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
	tmpl           *template.Template
	dryRun         bool
	pretendUsers   bool
}

type syncerParams struct {
	pdClient        *pagerDutyClient
	slClient        *slackMetaClient
	slackUsers      slackUsers
	slackUserGroups UserGroups
}

func (sp syncerParams) createSlackSyncs(ctx context.Context, cfg config) ([]runSlackSync, error) {
	var slSyncs []runSlackSync

	fmt.Println("Getting Slack channels")
	slChannels, err := sp.slClient.getChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get channels: %s", err)
	}
	fmt.Printf("Got %d Slack channel(s)\n", len(slChannels))

	for _, cfgSlSync := range cfg.SlackSyncs {
		slSync := runSlackSync{
			name:         cfgSlSync.Name,
			pretendUsers: cfgSlSync.PretendUsers,
			dryRun:       cfgSlSync.DryRun,
		}

		if cfgSlSync.Template == "" {
			fmt.Printf("Slack sync %s: skipping topic handling because template is undefined\n", slSync.name)
		} else {
			var err error
			slSync.tmpl, err = template.New("topic").Parse(cfgSlSync.Template)
			if err != nil {
				return nil, fmt.Errorf("failed to create slack sync %q: failed to parse template %q: %s", slSync.name, cfgSlSync.Template, err)
			}

			cfgChannel := cfgSlSync.Channel
			slChannel := slChannels.find(cfgChannel.ID, cfgChannel.Name)
			if slChannel == nil {
				return nil, fmt.Errorf("failed to create slack sync %q: failed to find configured Slack channel %s", slSync.name, cfgChannel)
			}
			slSync.slackChannelID = slChannel.ID
			fmt.Printf("Slack sync %s: found Slack channel %q (ID %s)\n", slSync.name, slChannel.Name, slChannel.ID)
		}

		pdSchedules := pdSchedules{}
		fmt.Printf("Slack sync %s: Getting PagerDuty schedules\n", slSync.name)
		for _, schedule := range cfgSlSync.Schedules {
			pdSchedule, err := sp.pdClient.getSchedule(schedule.ID, schedule.Name)
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

func (s *syncer) runSlackSync(ctx context.Context, slackSync runSlackSync) error {
	if !slackSync.dryRun {
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
	}

	ocgs := oncallGroups{}
	slackUserIDByScheduleName := map[string]string{}
	for _, schedule := range slackSync.pdSchedules {
		fmt.Printf("Processing schedule %s\n", schedule)
		onCallUser, err := s.pdClient.getOnCallUser(schedule)
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

	if slackSync.tmpl == nil {
		fmt.Println("Skipping topic update")
	} else {
		var buf bytes.Buffer
		fmt.Printf("Executing template with Slack user IDs by schedule name: %s\n", slackUserIDByScheduleName)
		err := slackSync.tmpl.Execute(&buf, slackUserIDByScheduleName)
		if err != nil {
			return fmt.Errorf("failed to render template: %s", err)
		}

		topic := buf.String()
		err = s.slClient.updateTopic(ctx, slackSync.slackChannelID, topic, slackSync.dryRun)
		if err != nil {
			return fmt.Errorf("failed to update topic: %s", err)
		}
	}

	return nil
}
