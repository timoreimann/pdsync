package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/matryer/try"
)

type pdSchedules []pdSchedule

func (schedules *pdSchedules) ensureSchedule(schedule pdSchedule) {
	for _, sched := range *schedules {
		if sched.id == schedule.id {
			return
		}
	}
	*schedules = append(*schedules, schedule)
}

type pdSchedule struct {
	id         string
	name       string
	userGroups UserGroups
}

func (ps pdSchedule) String() string {
	return fmt.Sprintf("{ID:%s Name:%q}", ps.id, ps.name)
}

func pagerDutyUserString(user pagerduty.User) string {
	return fmt.Sprintf("ID: %s Name: %s Email: %s", user.ID, user.Name, user.Email)
}

type pagerDutyClient struct {
	*pagerduty.Client
	pdSchedulesByNameOnce sync.Once
	pdSchedulesByName     map[string]pdSchedule
}

func newPagerDutyClient(token string) *pagerDutyClient {
	return &pagerDutyClient{
		Client: pagerduty.NewClient(token),
	}
}

func (cl *pagerDutyClient) getSchedule(ctx context.Context, id, name string) (*pdSchedule, error) {
	if id != "" {
		schedule, err := cl.getScheduleByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get schedule by ID: %s", err)
		}
		return schedule, nil
	}

	schedule, err := cl.getScheduleByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedules by name: %s", err)
	}

	return schedule, nil
}

func (cl *pagerDutyClient) getScheduleByID(ctx context.Context, scheduleID string) (*pdSchedule, error) {
	if scheduleID == "" {
		return nil, errors.New("schedule ID is missing")
	}

	fmt.Printf("Looking up schedule by ID %s\n", scheduleID)
	var schedule *pagerduty.Schedule
	rErr := retryOnPagerDutyRateLimit(func() error {
		var err error
		schedule, err = cl.GetScheduleWithContext(ctx, scheduleID, pagerduty.GetScheduleOptions{})
		return err
	})
	if rErr != nil {
		return nil, rErr
	}

	if schedule == nil {
		return nil, nil
	}

	return &pdSchedule{
		id:   schedule.ID,
		name: schedule.Name,
	}, nil
}

func (cl *pagerDutyClient) getScheduleByName(ctx context.Context, scheduleName string) (*pdSchedule, error) {
	if scheduleName == "" {
		return nil, errors.New("schedule name is missing")
	}

	var err error
	cl.pdSchedulesByNameOnce.Do(func() {
		cl.pdSchedulesByName, err = cl.getAllSchedulesByName(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get all schedules by name: %s", err)
	}

	pdSchedule, ok := cl.pdSchedulesByName[scheduleName]
	if !ok {
		return nil, nil
	}

	return &pdSchedule, nil
}

func (cl *pagerDutyClient) getAllSchedulesByName(ctx context.Context) (map[string]pdSchedule, error) {
	pdSchedules := map[string]pdSchedule{}
	opts := pagerduty.ListSchedulesOptions{
		Limit: 100,
	}
	fmt.Println("Collecting schedules")
	for {
		fmt.Println("Loading PagerDuty schedules page")
		var schedulesResp *pagerduty.ListSchedulesResponse
		rErr := retryOnPagerDutyRateLimit(func() error {
			var err error
			schedulesResp, err = cl.ListSchedulesWithContext(ctx, opts)
			return err
		})
		if rErr != nil {
			return nil, rErr
		}

		for _, schedule := range schedulesResp.Schedules {
			pdSchedules[schedule.Name] = pdSchedule{
				id:   schedule.ID,
				name: schedule.Name,
			}
		}

		if !schedulesResp.APIListObject.More {
			break
		}
		opts.Offset = opts.Offset + opts.Limit
	}

	return pdSchedules, nil
}

func (cl *pagerDutyClient) getOnCallUser(ctx context.Context, schedule pdSchedule) (pagerduty.User, error) {
	now := time.Now()
	fmt.Printf("Getting on-call users for schedule %s\n", schedule)
	onCallUsers, err := cl.ListOnCallUsersWithContext(ctx, schedule.id, pagerduty.ListOnCallUsersOptions{
		Since: now.Add(-1 * time.Second).Format(time.RFC3339),
		Until: now.Format(time.RFC3339),
	})
	if err != nil {
		return pagerduty.User{}, err
	}

	if len(onCallUsers) != 1 {
		return pagerduty.User{}, fmt.Errorf("unexpected number of on-call users: %d", len(onCallUsers))
	}

	onCallUser := onCallUsers[0]
	fmt.Printf("Got on-call user %q (ID %s) for schedule %s\n", onCallUser.Name, onCallUser.ID, schedule)

	return onCallUser, nil
}

func retryOnPagerDutyRateLimit(f func() error) error {
	return try.Do(func(attempt int) (retry bool, retryErr error) {
		err := f()
		if err != nil {
			if strings.Contains(err.Error(), fmt.Sprintf("HTTP response code: %d", http.StatusTooManyRequests)) {
				sleep := 1 * time.Minute
				fmt.Printf("PagerDuty rate limit hit -- waiting %s\n", sleep)
				time.Sleep(sleep)
				return true, err
			}
		}
		return false, err
	})
}
