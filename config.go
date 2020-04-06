package main

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

// ConfigSchedule represents a PagerDuty schedule identified by either ID or name.
type ConfigSchedule struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

// ConfigChannel represents a Slack channel identified by either ID or name.
type ConfigChannel struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

// ConfigSlackSync represents a synchronization between a set of PagerDuty schedules and a Slack channel.
type ConfigSlackSync struct {
	Name      string           `yaml:"name"`
	Schedules []ConfigSchedule `yaml:"schedules"`
	Channel   ConfigChannel    `yaml:"channel"`
	Template  string           `yaml:"template"`
	DryRun    bool             `yaml:"dryRun"`
}

type config struct {
	SlackSyncs []ConfigSlackSync `yaml:"slackSyncs"`
}

func generateConfig(p params) (config, error) {
	var (
		cfg config
		err error
	)

	if p.config != "" {
		cfg, err = readConfigFile(p.config)
		if err != nil {
			return config{}, err
		}
	} else {
		if p.tmplFile != "" {
			b, err := ioutil.ReadFile(p.tmplFile)
			if err != nil {
				return config{}, err
			}
			p.tmplString = string(b)
		}
		cfg = singleSlackSync(p)
	}

	// If specified, let the global dry-run parameter override per-sync dry-run
	// values.
	if p.dryRun != nil {
		for i := range cfg.SlackSyncs {
			cfg.SlackSyncs[i].DryRun = *p.dryRun
		}
	}

	err = validateConfig(&cfg)
	if err != nil {
		return config{}, err
	}

	return cfg, err
}

func readConfigFile(file string) (config, error) {
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return config{}, err
	}

	var cfg config
	err = yaml.Unmarshal(content, &cfg)
	return cfg, err
}

func singleSlackSync(p params) config {
	slackSync := ConfigSlackSync{
		Name: "default",
		Channel: ConfigChannel{
			ID:   p.channelID,
			Name: p.channelName,
		},
		Template: p.tmplString,
	}
	for _, scheduleID := range p.scheduleIDs {
		slackSync.Schedules = append(slackSync.Schedules, ConfigSchedule{ID: scheduleID})
	}
	for _, scheduleName := range p.scheduleNames {
		slackSync.Schedules = append(slackSync.Schedules, ConfigSchedule{Name: scheduleName})
	}

	return config{
		SlackSyncs: []ConfigSlackSync{slackSync},
	}
}

func validateConfig(cfg *config) error {
	foundNames := map[string]bool{}
	for _, sync := range cfg.SlackSyncs {
		if _, ok := foundNames[sync.Name]; ok {
			return fmt.Errorf("slack sync name %q already used", sync.Name)
		}
		foundNames[sync.Name] = true

		for _, cfgSchedule := range sync.Schedules {
			if cfgSchedule.ID == "" && cfgSchedule.Name == "" {
				return fmt.Errorf("slack sync %q invalid: must specify either schedule ID or schedule name", sync.Name)
			}
		}

		if sync.Channel.ID == "" && sync.Channel.Name == "" {
			return fmt.Errorf("slack sync %q invalid: must specify either channel ID or channel name", sync.Name)
		}

		if sync.Template == "" {
			return fmt.Errorf("slack sync %q invalid: template is missing", sync.Name)
		}
	}

	return nil
}
