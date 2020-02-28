package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/matryer/try"
)

type pdSchedules []pdSchedule

func (schedules *pdSchedules) ensureSchedule(schedule pdSchedule) {
	for _, sched := range *schedules {
		if sched == schedule {
			return
		}
	}
	*schedules = append(*schedules, schedule)
}

type pdSchedule struct {
	id   string
	name string
}

type pdUser struct {
	name  string
	email string
}

func pagerDutyUserString(user pagerduty.User) string {
	return fmt.Sprintf("ID: %s Name: %s Email: %s", user.ID, user.Name, user.Email)
}

type pagerDutyClient struct {
	*pagerduty.Client
}

func newPagerDutyClient(token string) *pagerDutyClient {
	return &pagerDutyClient{
		Client: pagerduty.NewClient(token),
	}
}

func (cl *pagerDutyClient) getSchedules(ids, names []string) (pdSchedules, error) {
	pdSchedules, err := cl.getSchedulesByID(ids)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedules by ID: %s", err)
	}

	schedsByName, err := cl.getSchedulesByName(names)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedules by name: %s", err)
	}

	for _, schedByName := range schedsByName {
		pdSchedules.ensureSchedule(schedByName)
	}

	return pdSchedules, nil
}

func (cl *pagerDutyClient) getSchedulesByID(scheduleIDs []string) (pdSchedules, error) {
	var pdSchedules pdSchedules
	for _, scheduleID := range scheduleIDs {
		fmt.Printf("Looking up schedule by ID %s\n", scheduleID)
		var schedule *pagerduty.Schedule
		rErr := retryOnPagerDutyRateLimit(func() error {
			var err error
			schedule, err = cl.GetSchedule(scheduleID, pagerduty.GetScheduleOptions{})
			return err
		})
		if rErr != nil {
			return nil, rErr
		}

		if schedule == nil {
			return nil, fmt.Errorf("schedule by ID %s not found", scheduleID)
		}

		pdSchedules = append(pdSchedules, pdSchedule{
			id:   schedule.ID,
			name: schedule.Name,
		})
	}

	return pdSchedules, nil
}

func (cl *pagerDutyClient) getSchedulesByName(scheduleNames []string) (pdSchedules, error) {
	if len(scheduleNames) == 0 {
		return nil, nil
	}

	var pdSchedules pdSchedules
	alo := pagerduty.APIListObject{
		Limit: 100,
	}
	fmt.Printf("Looking up schedules %q by name\n", scheduleNames)
	for {
		fmt.Println("Loading PagerDuty schedules page")
		var schedulesResp *pagerduty.ListSchedulesResponse
		rErr := retryOnPagerDutyRateLimit(func() error {
			var err error
			schedulesResp, err = cl.ListSchedules(pagerduty.ListSchedulesOptions{APIListObject: alo})
			return err
		})
		if rErr != nil {
			return nil, rErr
		}

		for _, schedule := range schedulesResp.Schedules {
			if !isRelevantSchedule(schedule.Name, scheduleNames) {
				continue
			}

			pdSchedules = append(pdSchedules, pdSchedule{
				id:   schedule.ID,
				name: schedule.Name,
			})
		}

		if !schedulesResp.APIListObject.More {
			break
		}
		alo.Offset = alo.Offset + alo.Limit
	}

	var missingNames []string
Loop:
	for _, scheduleName := range scheduleNames {
		for _, pdSchedule := range pdSchedules {
			if scheduleName == pdSchedule.name {
				continue Loop
			}
		}
		missingNames = append(missingNames, scheduleName)
	}
	if len(missingNames) > 0 {
		return nil, fmt.Errorf("failed to look up the following schedule(s) by name: %s", missingNames)
	}

	return pdSchedules, nil
}

func (cl *pagerDutyClient) getOnCallUsersBySchedule(pdSchedules pdSchedules) (map[pdSchedule]pagerduty.User, error) {
	onCallUserBySchedule := map[pdSchedule]pagerduty.User{}
	now := time.Now()
	for _, schedule := range pdSchedules {
		fmt.Printf("Getting on-call users for schedule %#v\n", schedule)
		onCallUsers, err := cl.ListOnCallUsers(schedule.id, pagerduty.ListOnCallUsersOptions{
			Since: now.Add(-1 * time.Second).Format(time.RFC3339),
			Until: now.Format(time.RFC3339),
		})
		if err != nil {
			return nil, err
		}

		if len(onCallUsers) != 1 {
			return nil, fmt.Errorf("unexpected number of on-call users: %d", len(onCallUsers))
		}

		onCallUser := onCallUsers[0]
		fmt.Printf("Got on-call user %q (ID %s) for schedule %#v\n", onCallUser.Name, onCallUser.ID, schedule)
		onCallUserBySchedule[schedule] = onCallUser
	}

	return onCallUserBySchedule, nil
}

func isRelevantSchedule(s string, schedules []string) bool {
	for _, schedule := range schedules {
		if schedule == s {
			return true
		}
	}
	return false
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
