package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

type channelList []slack.Channel

func (cl channelList) find(id, name string) *slack.Channel {
	if id == "" && name == "" {
		return nil
	}

	for _, ch := range cl {
		if ch.ID == id || ch.Name == name {
			return &ch
		}
	}

	return nil
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
	// GetUsersContext does retrying on rate-limit errors, so no need to wrap it
	// around retryOnSlackRateLimit.
	apiUsers, err := cl.GetUsersContext(ctx)
	if err != nil {
		return nil, err
	}

	slUsers := make(slackUsers, 0, len(apiUsers))
	for _, apiUser := range apiUsers {
		slUsers = append(slUsers, createSlackUser(apiUser))
	}

	return slUsers, nil
}

func (cl *slackClient) getChannels(ctx context.Context) (channelList, error) {
	var (
		list   channelList
		cursor string
	)

	for {
		var (
			channels   []slack.Channel
			nextCursor string
		)
		retErr := retryOnSlackRateLimit(ctx, func(ctx context.Context) error {
			var err error
			channels, nextCursor, err = cl.GetConversationsContext(ctx, &slack.GetConversationsParameters{
				Cursor:          cursor,
				ExcludeArchived: "true",
				Limit:           200,
				Types:           []string{"public_channel", "private_channel"},
			})
			return err
		})
		if retErr != nil {
			return nil, retErr
		}

		for _, channel := range channels {
			list = append(list, channel)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return list, nil
}

func (cl *slackClient) getChannelByID(ctx context.Context, id string) (*slack.Channel, error) {
	return cl.GetConversationInfoContext(ctx, id, false)
}

func (cl *slackClient) getUserGroups(ctx context.Context) ([]UserGroup, error) {
	var groups []slack.UserGroup
	retErr := retryOnSlackRateLimit(ctx, func(ctx context.Context) error {
		var err error
		groups, err = cl.GetUserGroupsContext(ctx, []slack.GetUserGroupsOption(nil)...)
		return err
	})
	if retErr != nil {
		return nil, retErr
	}

	userGroups := make([]UserGroup, 0, len(groups))
	for _, group := range groups {
		userGroups = append(userGroups, UserGroup{
			ID:     group.ID,
			Name:   group.Name,
			Handle: group.Handle,
		})
	}

	return userGroups, nil
}

type oncallGroups []*oncallGroup

func (ocgs *oncallGroups) getOrCreate(ug UserGroup) *oncallGroup {
	for _, ocg := range *ocgs {
		if ocg.userGroupID == ug.ID {
			return ocg
		}
	}

	ocg := &oncallGroup{
		userGroupID:   ug.ID,
		userGroupName: ug.Name,
	}
	*ocgs = append(*ocgs, ocg)
	return ocg
}

type oncallGroup struct {
	userGroupID   string
	userGroupName string
	members       []string
}

func (ocg *oncallGroup) ensureMember(m string) {
	for _, memb := range ocg.members {
		if memb == m {
			return
		}
	}
	ocg.members = append(ocg.members, m)
}

func (cl *slackClient) updateOncallGroupMembers(ctx context.Context, oncallGroups oncallGroups, dryRun bool) error {
	for _, group := range oncallGroups {
		currentMembers, err := cl.GetUserGroupMembersContext(ctx, group.userGroupID)
		if err != nil {
			return fmt.Errorf("failed to get user group members for %q: %s", group.userGroupName, err)
		}
		if cmp.Equal(currentMembers, group.members, cmpopts.SortSlices(func(x, y string) bool {
			return x < y
		})) {
			fmt.Printf("User group %q already has the right members\n", group.userGroupName)
			continue
		}
		concatMembers := strings.Join(group.members, ",")
		if dryRun {
			fmt.Printf("[DRY RUN] Not updating user group %s with member(s): %s\n", group.userGroupName, concatMembers)
			continue
		}
		_, err = cl.UpdateUserGroupMembersContext(ctx, group.userGroupID, concatMembers)
		if err != nil {
			return fmt.Errorf("failed to update user group members for %q: %s", group.userGroupName, err)
		}
		fmt.Printf("Updated user group %s with member(s): %s\n", group.userGroupName, concatMembers)
	}

	return nil
}

func (cl *slackClient) updateTopic(ctx context.Context, channelID string, topic string, dryRun bool) error {
	channel, err := cl.getChannelByID(ctx, channelID)
	if err != nil {
		return err
	}

	if channel.Topic.Value == topic {
		fmt.Println("Topic already set correctly")
	} else {
		fmt.Printf("Updating topic from\n[BEGIN-OF-OLD]\n%s\n[END-OF-OLD]\nto:\n[BEGIN-OF-NEW]\n%s\n[END-OF-NEW]\n", channel.Topic.Value, topic)
		if dryRun {
			fmt.Println("[DRY RUN] Not updating topic")
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
				sleep := rle.RetryAfter
				fmt.Printf("Slack rate limit hit -- waiting %s\n", sleep)
				time.Sleep(sleep)
				return true, err
			}
		}
		return false, err
	})
}
