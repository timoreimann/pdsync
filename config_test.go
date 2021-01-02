package main

import (
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
			name:       "none of id or name specifiers given",
			inSchedule: "foo=bar",
			wantErrStr: `one of "id" or "name" must be given`,
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
				ID:         "schedule",
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
