package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		name       string
		inSchedule string
		wantErrStr string
		wantCfg    ConfigSchedule
	}{
		{
			name:       "missing separator on schedule specifier",
			inSchedule: "missing-schedule-separator",
			wantErrStr: "missing separator on element",
		},
		{
			name:       "multiple id specifiers given",
			inSchedule: "id=schedule1;id=schedule2",
			wantErrStr: `multiple values for key "id" not allowed`,
		},
		{
			name:       "multiple name specifiers given",
			inSchedule: "name=schedule1;name=schedule2",
			wantErrStr: `multiple values for key "name" not allowed`,
		},
		{
			name:       "id and name specifiers given",
			inSchedule: "id=schedule;name=schedule",
			wantErrStr: `"id" and "name" cannot be specified simultaneously`,
		},
		{
			name:       "missing separator on user group specifier",
			inSchedule: "id=schedule;userGroup=missing-usergroup-separator",
			wantErrStr: "does not follow key=value pattern",
		},
		{
			name:       "unsupported user group specifier",
			inSchedule: "id=schedule;userGroup=color=green",
			wantErrStr: `has unexpected key "color"`,
		},
		{
			name:       "unsupported key/value pair",
			inSchedule: "id=schedule;foo=bar",
			wantErrStr: "unsupported key/value pairs left",
		},
		{
			name:       "valid schedule with all user group specifiers",
			inSchedule: "id=schedule;userGroup=id=123;userGroup=name=user group 2;userGroup=handle=my-ug",
			wantCfg: ConfigSchedule{
				ID: "schedule",
				UserGroups: UserGroups{
					{
						ID: "123",
					},
					{
						Name: "user group 2",
					},
					{
						Handle: "my-ug",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCfg, err := parseSchedule(tt.inSchedule)
			if tt.wantErrStr != "" {
				var gotErrStr string
				if err != nil {
					gotErrStr = err.Error()
				}
				if !strings.Contains(gotErrStr, tt.wantErrStr) {
					t.Errorf("got error string %q, want %q", gotErrStr, tt.wantErrStr)
				}
			} else if diff := cmp.Diff(tt.wantCfg, gotCfg); diff != "" {
				t.Errorf("ConfigSchedule mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPopulateChannel(t *testing.T) {
	allChannels := channelList{}
	err := json.Unmarshal([]byte(`[{ "id": "1", "name": "Foo" }, { "name": "Bar", "id": "2" }]`), &allChannels)
	if err != nil {
		t.Fatalf("Unable to convert json to slack: %s", err)
	}

	tests := []struct {
		title       string
		name        string
		id          string
		wantErrStr  string
		wantChannel *ConfigChannel
	}{
		{
			title:      "no match",
			name:       "foo",
			wantErrStr: `failed to find configured Slack channel {ID: Name:"foo"}`,
		},
		{
			title:       "By Name",
			name:        "Foo",
			wantChannel: &ConfigChannel{Name: "Foo", ID: "1"},
		},
		{
			title:       "By ID",
			id:          "2",
			wantChannel: &ConfigChannel{Name: "Bar", ID: "2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			cfg := &ConfigSlackSync{
				Channel: &ConfigChannel{
					ID:   tt.id,
					Name: tt.name,
				},
			}
			err := cfg.populateChannel(context.Background(), allChannels)
			if tt.wantErrStr != "" {
				var gotErrStr string
				if err != nil {
					gotErrStr = err.Error()
				}
				if !strings.Contains(gotErrStr, tt.wantErrStr) {
					t.Errorf("got error string %q, want %q", gotErrStr, tt.wantErrStr)
				}
			} else if diff := cmp.Diff(tt.wantChannel, cfg.Channel); diff != "" {
				t.Errorf("ConfigChannel mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
