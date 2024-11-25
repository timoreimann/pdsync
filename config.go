package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v2"
)

// ConfigSchedule represents a PagerDuty schedule identified by either ID or name.
type ConfigSchedule struct {
	ID         string     `yaml:"id"`
	Name       string     `yaml:"name"`
	UserGroups UserGroups `yaml:"userGroups"`
}

func (cs ConfigSchedule) String() string {
	return fmt.Sprintf("{ID:%s Name:%q}", cs.ID, cs.Name)
}

type UserGroups []UserGroup

func (ugs UserGroups) find(ug2 UserGroup) *UserGroup {
	for _, ug := range ugs {
		if (ug2.ID != "" && ug.ID == ug2.ID) ||
			(ug2.Name != "" && ug.Name == ug2.Name) ||
			(ug2.Handle != "" && ug.Handle == ug2.Handle) {
			return &ug
		}
	}
	return nil
}

func (ugs *UserGroups) getOrCreate(ug2 UserGroup) *UserGroup {
	if ugs == nil {

	}

	for _, ug := range *ugs {
		if (ug2.ID != "" && ug.ID == ug2.ID) ||
			(ug2.Name != "" && ug.Name == ug2.Name) ||
			(ug2.Handle != "" && ug.Handle == ug2.Handle) {
			return &ug
		}
	}
	return nil
}

type UserGroup struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name"`
	Handle string `yaml:"handle"`
}

func (ug UserGroup) String() string {
	return fmt.Sprintf("{ID:%s Name:%q Handle:%s}", ug.ID, ug.Name, ug.Handle)
}

// ConfigChannel represents a Slack channel identified by either ID or name.
type ConfigChannel struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

func (cc ConfigChannel) String() string {
	return fmt.Sprintf("{ID:%s Name:%q}", cc.ID, cc.Name)
}

// ConfigSlackSync represents a synchronization between a set of PagerDuty schedules and a Slack channel.
type ConfigSlackSync struct {
	Name           string             `yaml:"name"`
	Schedules      []ConfigSchedule   `yaml:"schedules"`
	Channel        *ConfigChannel     `yaml:"channel"`
	Template       *template.Template `yaml:"-"`
	templateString string             `yaml:"template"`
	PretendUsers   bool               `yaml:"pretendUsers"`
	DryRun         bool               `yaml:"dryRun"`
}

func (css *ConfigSlackSync) populateChannel(_ context.Context, allChannels channelList) error {
	if css.Channel == nil {
		return nil
	}

	slChannel := allChannels.find(css.Channel.ID, css.Channel.Name)
	if slChannel == nil {
		return fmt.Errorf("failed to find configured Slack channel %s", css.Channel)
	}

	css.Channel = &ConfigChannel{
		ID:   slChannel.ID,
		Name: slChannel.Name,
	}

	fmt.Printf("Slack sync %s: found Slack channel %q (ID %s)\n", css.Name, css.Channel.Name, css.Channel.ID)
	return nil
}

func (css *ConfigSlackSync) populateTemplate(_ context.Context) error {
	if css.templateString == "" {
		return nil
	}

	var err error
	css.Template, err = template.New("topic").Parse(css.templateString)
	if err != nil {
		return fmt.Errorf("failed to parse %s's template %q: %s", css.Name, css.templateString, err)
	}

	return nil
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
			b, err := os.ReadFile(p.tmplFile)
			if err != nil {
				return config{}, err
			}
			p.tmplString = string(b)
		}
		cfg, err = singleSlackSync(p)
		if err != nil {
			return config{}, err
		}
	}

	// Let globally defined parameters override per-sync ones.

	if p.pretendUsers != nil {
		for i := range cfg.SlackSyncs {
			cfg.SlackSyncs[i].PretendUsers = *p.pretendUsers
		}
	}

	if p.dryRun != nil {
		for i := range cfg.SlackSyncs {
			cfg.SlackSyncs[i].DryRun = *p.dryRun
		}
	}

	return cfg, err
}

func readConfigFile(file string) (config, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return config{}, err
	}

	var cfg config
	err = yaml.Unmarshal(content, &cfg)
	return cfg, err
}

func singleSlackSync(p params) (config, error) {
	slackSync := ConfigSlackSync{
		Name: "default",
	}

	if len(p.channelID) != 0 || len(p.channelName) != 0 {
		slackSync.Channel = &ConfigChannel{
			ID:   p.channelID,
			Name: p.channelName,
		}
	}
	for _, schedule := range p.schedules {
		cfgSchedule, err := parseSchedule(schedule)
		if err != nil {
			return config{}, err
		}
		slackSync.Schedules = append(slackSync.Schedules, cfgSchedule)
	}

	return config{
		SlackSyncs: []ConfigSlackSync{slackSync},
	}, nil
}

func parseSchedule(schedule string) (ConfigSchedule, error) {
	kvs := map[string][]string{}
	for _, elem := range strings.Split(schedule, ";") {
		kv := strings.SplitN(elem, "=", 2)
		if len(kv) < 2 {
			return ConfigSchedule{}, fmt.Errorf("missing separator on element %q", elem)
		}
		key := kv[0]
		value := kv[1]
		kvs[key] = append(kvs[key], value)
	}

	var id string
	if ids := kvs["id"]; len(ids) > 0 {
		if len(ids) > 1 {
			return ConfigSchedule{}, errors.New(`multiple values for key "id" not allowed`)
		}
		id = ids[0]
		delete(kvs, "id")
	}
	var name string
	if names := kvs["name"]; len(names) > 0 {
		if len(names) > 1 {
			return ConfigSchedule{}, errors.New(`multiple values for key "name" not allowed`)
		}
		name = names[0]
		delete(kvs, "name")
	}

	if id != "" && name != "" {
		return ConfigSchedule{}, errors.New(`"id" and "name" cannot be specified simultaneously`)
	}

	cfgSchedule := ConfigSchedule{
		ID:   id,
		Name: name,
	}

	for _, userGroup := range kvs["userGroup"] {
		kv := strings.Split(userGroup, "=")
		if len(kv) != 2 {
			return ConfigSchedule{}, fmt.Errorf("user group %s does not follow key=value pattern", userGroup)
		}
		ugKey := kv[0]
		ugValue := kv[1]

		var ug UserGroup
		switch ugKey {
		case "id":
			ug.ID = ugValue
		case "name":
			ug.Name = ugValue
		case "handle":
			ug.Handle = ugValue
		default:
			return ConfigSchedule{}, fmt.Errorf("user group %s has unexpected key %q", userGroup, ugKey)
		}
		cfgSchedule.UserGroups = append(cfgSchedule.UserGroups, ug)
	}
	delete(kvs, "userGroup")

	if len(kvs) > 0 {
		return ConfigSchedule{}, fmt.Errorf("unsupported key/value pairs left: %s", kvs)
	}

	return cfgSchedule, nil
}

func (cfg *config) validateConfig() error {
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
			for _, cfgUserGroup := range cfgSchedule.UserGroups {
				if cfgUserGroup.ID == "" && cfgUserGroup.Name == "" && cfgUserGroup.Handle == "" {
					return fmt.Errorf("slack sync %q user group %s invalid: must specify either user group ID or user group name or user group handle", sync.Name, cfgUserGroup)
				}
			}
		}

		channelGiven := sync.Channel != nil
		if sync.Template != nil {
			if !channelGiven {
				return fmt.Errorf("slack sync %q invalid: must specify either channel ID or channel name when topic is given", sync.Name)
			}
		} else if channelGiven {
			return fmt.Errorf("slack sync %q invalid: must specify template when either channel ID or channel name is given", sync.Name)
		}
	}

	return nil
}
