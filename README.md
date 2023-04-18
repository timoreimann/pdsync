# pdsync

_pdsync_ is a tool to synchronize on-call schedules in [PagerDuty](https://www.pagerduty.com/) into third-party systems.

Right now, the only supported target is Slack: given a list of PageDuty schedules, a Slack channel, and a template, _pdsync_ will periodically poll the on-call personnel on the schedules and update the Slack channel's topic. The template accepts a variable that matches a PageDuty schedule name to fill in the corresponding on-call Slack handles. Additionally, pre-existing user groups can be updated automatically to always point to the current on-call personnel.

## How-to

You will need to create a Slack app with the following scopes:

| Scope              | Optional | Used for                              |
|--------------------|----------|---------------------------------------|
| `users:read`       | no       |                                       |
| `channels:join`    | yes      | joining public channels automatically |
| `channels:read`    | yes      | managing topics (public channels)     |
| `channels:manage`  | yes      | managing topics (public channels)     |
| `groups:read`      | yes      | managing topics (private channels)    |
| `groups:write`     | yes      | managing topics (private channels)    |
| `usergroups:read`  | yes      | managing user groups                  |
| `usergroups:write` | yes      | managing user groups                  |

For private channels and when the `channels:join` scope is not assigned, the Slack app needs to be joined to the target channel manually. (One easy to do this is to select the app from a channel where it already exists and use the context menu to add it to another channel.)

Support for private channels also needs the `--include-private-channels` flag to be explicitly set.

Next up, one or more _slack syncs_ must be configured, preferrably through a YAML configuration file. Here is an [example file](config.example.yaml):

```yaml
slackSyncs:
    # name must be unique across all given syncs
  - name: team-awesome
    schedules:
      # a schedule can be given by name
    - name: Awesome-Primary
      # a schedule may optionally have one or more user groups that get updated with the on-call personnel
      userGroups:
      # choose one of `id`, `name`, or `handle` to reference a pre-existing user group
      - name: Team Awesome On-call (all)
      - handle: team-awesome-on-call-primary
      # alternatively, the schedule can be given by ID (the name is assumed to be Awesome-Secondary and referenced in the template below)
    - id: D34DB33F
      userGroups:
        # the first user group is also defined in the primary schedule above
      - name: Team Awesome On-call (all)
      - handle: team-awesome-on-call-secondary
    channel:
      name: awesome
      # a channel can also be provided by ID:
      # id: 1A3F8FGJ

      # The on-call Slack user variables are named after the schedule names.
      # Note how it is called ".AwesomePrimary" and not ".Awesome-Primary" because Go
      # template variables support alphanumeric characters only, so pdsync
      # strips off all illegal characters.
    template: |-
      primary on-call: <@{{.AwesomePrimary}}> (Slack handle: @team-awesome-on-call-primary)
      secondary on-call: <@{{.AwesomeSecondary}}> (Slack handle: @team-awesome-on-call-secondary)

      reach out to both primary and secondary via @team-awesome-on-call
    # Set to true to skip updating the Slack channel topic
    dryRun: false
```

Now pdsync can be started like the following:

```shell
pdsync --config config.example.yaml
```

This will update the topic of the `awesome` Slack channel mentioning the primary and secondary on-call Slack handles. The template variables match the PagerDuty schedule names.

**Note:** Go template variables take alphanumeric names only. _pdsync_ exposes channel names without unsupported characters in the template variables, which is why you will need to use `{{.AwesomePrimary}}` (as opposed to `{{.Awesome-Primary}}`) in the example above.

The example will also update three Slack user groups to make it easy to ping the current primary, secondary, and all on-call personnel.

For simple cases and testing purposes, it is also possible to specify a single slack sync through CLI parameters:

```shell
pdsync --schedule 'name=Awesome-Primary;userGroup=handle=team-awesome-oncall-primary' --schedule='id=D34DB33F;userGroup=name=Team Awesome On-call Secondary' --channel-name awesome --template 'primary on-call: <@{{.AwesomePrimary}}> secondary on-call: <@{{.AwesomeSecondary}}>'
```

The `--schedule` flag consists of a series of the following key/value pairs:

- `id=<schedule reference>`: the ID of a PagerDuty schedule (mutually exclusive with `name=` below)
- `name=<schedule reference>`: the name of a PagerDuty schedule (mutually exclusive with `id=` above)
- `userGroup=<key identifier>=<user group reference>`: the `id`, `name`, or `handle` (i.e., the `<key identifier>`) of a user group; can be repeated to reference multiple user groups

Add `--dry-run` to turn all mutating API requests into no-ops.

Run the tool with `--help` for details.

## status

This tool is newish, has too few tests, and is possibly buggy. However, it did get quite some mileage already.

All things considered, use it at your own risk -- but do provide feedback.
