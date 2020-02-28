# pdsync

_pdsync_ is a tool to synchronize on-call schedules in [PagerDuty](https://www.pagerduty.com/) into third-party systems.

Right now, the only supported target is Slack: given a list of PageDuty schedules, a Slack channel, and a template, _pdsync_ will periodically poll the on-call personnel on the schedules and update the Slack channel's topic. The template accepts a variable that matches a PageDuty schedule name to fill in the corresponding on-call Slack handles.

You will need to create a Slack app with the following permissions

- `users:read`
- `channels:read`
- `channels:manage`
- `groups:read`
- `groups:write`

(`groups.*` is only needed for private channels)

and invite it to the target channel. Afterwards, you can run something like the following:

```shell
pdsync --schedule-names Team-Primary --schedule-names Team-Secondary --channel-name team-awesome --template 'primary on-call: <@{{.TeamPrimary}}> secondary on-call: <@{{.TeamSecondary}}>'
```

(Add `--dry-run` to make this a no-op.)

This will update the topic of the `team-awesome` Slack channel mentioning the primary and secondary on-call Slack handles. The template variables match the PagerDuty schedule names.

**Note:** Go template variables take alphanumeric names only. _pdsync_ exposes channel names without unsupported characters in the template variables, which is why you will need to use `{{.TeamPrimary}}` (as opposed to `{{.Team-Primary}}`) above.

Run the tool with `--help` for details.

## status

This tool is newish, full of bugs, and without tests. Use it at your own risk but do provide feedback!
