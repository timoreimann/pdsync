package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/matryer/try"

	"github.com/PagerDuty/go-pagerduty"

	"github.com/slack-go/slack"
)

type slackUsers []slackUser

func (users slackUsers) findByPDUser(pdUser pagerduty.User) *slackUser {
	for _, slackUser := range users {
		if slackUser.email == pdUser.Email ||
			slackUser.realName == strings.ToLower(pdUser.Name) ||
			slackUser.name == strings.ToLower(pdUser.Name) {
			return &slackUser
		}
	}

	return nil
}

type slackUser struct {
	id       string
	name     string
	realName string
	email    string
}

type slackClient struct {
	*slack.Client
}

func newSlackClient(token string) *slackClient {
	return &slackClient{
		Client: slack.New(token),
	}
}

func (cl *slackClient) getSlackUsers(ctx context.Context) (slackUsers, error) {
	var apiUsers []slack.User
	rErr := retryOnSlackRateLimit(ctx, func(ctx context.Context) error {
		var err error
		apiUsers, err = cl.GetUsersContext(ctx)
		return err
	})
	if rErr != nil {
		return nil, rErr
	}

	slUsers := make(slackUsers, 0, len(apiUsers))
	for _, apiUser := range apiUsers {
		slUsers = append(slUsers, createSlackUser(apiUser))
	}

	return slUsers, nil
}

func (cl *slackClient) getChannel(ctx context.Context, name, id string) (*slack.Channel, error) {
	var (
		slChannel *slack.Channel
		err       error
	)
	if id != "" {
		fmt.Printf("Looking up channel by ID %s\n", id)
		slChannel, err = cl.getChannelByID(ctx, id)
	} else {
		fmt.Printf("Looking up channel by name %q\n", name)
		slChannel, err = cl.getChannelByName(ctx, name)
	}

	return slChannel, err
}

func (cl *slackClient) getChannelByID(ctx context.Context, id string) (*slack.Channel, error) {
	return cl.GetConversationInfoContext(ctx, id, false)
}

func (cl *slackClient) getChannelByName(ctx context.Context, name string) (*slack.Channel, error) {
	var (
		slChannel *slack.Channel
		cursor    string
	)

Loop:
	for {
		var (
			nextCursor string
			channels   []slack.Channel
		)
		rErr := retryOnSlackRateLimit(ctx, func(ctx context.Context) error {
			var err error
			channels, nextCursor, err = cl.GetConversationsContext(ctx, &slack.GetConversationsParameters{
				Cursor:          cursor,
				ExcludeArchived: "true",
				Types:           []string{"public_channel", "private_channel"},
			})
			return err
		})
		if rErr != nil {
			return nil, rErr
		}

		for _, channel := range channels {
			if channel.Name == name {
				slChannel = &channel
				break Loop
			}
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	if slChannel == nil {
		return nil, errors.New("failed to find channel")
	}

	return slChannel, nil
}

func (cl *slackClient) ensureTopic(ctx context.Context, channel *slack.Channel, topic string, dryRun bool) error {
	channel, err := cl.getChannelByID(ctx, channel.ID)
	if err != nil {
		return err
	}

	if channel.Topic.Value == topic {
		fmt.Println("Topic already set correctly")
	} else {
		fmt.Printf("Updating topic from\n[BEGIN-OF-OLD]\n%s\n[END-OF-OLD]\nto:\n[BEGIN-OF-NEW]\n%s\n[END-OF-NEW]\n", channel.Topic.Value, topic)
		if dryRun {
			fmt.Println("[DRY RUN] Not updating")
			return nil
		}
		_, err := cl.SetTopicOfConversationContext(ctx, channel.ID, topic)
		if err != nil {
			return err
		}
		fmt.Println("Topic updated")
	}

	return nil
}

func createSlackUser(apiUser slack.User) slackUser {
	return slackUser{
		id:       apiUser.ID,
		name:     strings.ToLower(apiUser.Name),
		realName: strings.ToLower(apiUser.RealName),
		email:    apiUser.Profile.Email,
	}
}

func retryOnSlackRateLimit(ctx context.Context, f func(ctx context.Context) error) error {
	return try.Do(func(attempt int) (retry bool, retryErr error) {
		err := f(ctx)
		if err != nil {
			var rle *slack.RateLimitedError
			if errors.As(err, &rle) {
				sleep := rle.RetryAfter * time.Nanosecond
				fmt.Printf("Slack rate limit hit -- waiting %s\n", sleep)
				time.Sleep(sleep)
				return true, err
			}
		}
		return false, err
	})
}
