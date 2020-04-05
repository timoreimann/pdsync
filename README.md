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

and invite it to the target channel.

Next up, one or more _slack syncs_ must be configured, preferrably through a YAML configuration file. Here is an [example file](config.example.yaml):

```yaml
slackSyncs:
    # name must be unique across all given syncs
  - name: team-awesome
    schedules:
      # a schedule can be given by name...
    - name: Awesome-Primary
      # ...or by ID
    - id: D34DB33F
    channel:
      name: awesome
      # a channel can also be provided by ID:
      # id: 1A3F8FGJ

      # The on-call Slack user variables are named after the schedule names.
      # Note how it is called ".AwesomePrimary" and not ".Awesome-Primary" because Go
      # template variables support alphanumeric characters only, so pdsync
      # strips off all illegal characters.
    template: |-
      primary on-call: <@{{.AwesomePrimary}}> secondary on-call: <@{{.AwesomeSecondary}}>
    # Set to true to skip updating the Slack channel topic
    dryRun: false
```

Now pdsync can be started like the following:

```shell
pdsync --config config.example.yaml
```

This will update the topic of the `awesome` Slack channel mentioning the primary and secondary on-call Slack handles. The template variables match the PagerDuty schedule names.

**Note:** Go template variables take alphanumeric names only. _pdsync_ exposes channel names without unsupported characters in the template variables, which is why you will need to use `{{.AwesomePrimary}}` (as opposed to `{{.Awesome-Primary}}`) in the example above.

It is also possible to specify a single slack sync through CLI parameters:

```shell
pdsync --schedule-names Awesome-Primary --schedule-ids D34DB33F --channel-name awesome --template 'primary on-call: <@{{.AwesomePrimary}}> secondary on-call: <@{{.AwesomeSecondary}}>'
```

(Add `--dry-run` to make this a no-op.)

Run the tool with `--help` for details.

## status

This tool is newish, full of bugs, and without tests. Use it at your own risk but do provide feedback!
