package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/ears"
	"gopkg.in/yaml.v3"
)

// Actors is the glossary of the actors a rule may name instead of "the
// system" (codetospec.yaml actors), each with its article. An entry is
// what the actor is, alone or with the other names a rule may give it,
// which stand for it:
//
//	actors:
//	  the scheduler: Runs the nightly jobs.
//	  the notifier:
//	    description: Pages the owner.
//	    aliases: [the pager]
type Actors struct {
	Descriptions map[string]string // name → what the actor is
	Aliases      map[string]string // alias → name; nil when none
}

// actorEntry is one entry of the glossary in its long form.
type actorEntry struct {
	Description string   `yaml:"description"`
	Aliases     []string `yaml:"aliases"`
}

// UnmarshalYAML reads the glossary and checks it before any model is
// called: every name and alias opens a requirement's subject
// (ears.ValidateActor), and an alias is no name and stands for one actor
// only, so a rule naming it has one meaning.
func (a *Actors) UnmarshalYAML(n *yaml.Node) error {
	var raw map[string]yaml.Node
	if err := n.Decode(&raw); err != nil {
		return err
	}
	a.Descriptions = make(map[string]string, len(raw))
	entries := make(map[string]actorEntry, len(raw))
	for name, value := range raw {
		var e actorEntry
		var err error
		if value.Kind == yaml.ScalarNode {
			err = value.Decode(&e.Description)
		} else {
			err = value.Decode(&e)
		}
		if err != nil {
			return fmt.Errorf("actors: %q: %w", name, err)
		}
		entries[name] = e
		a.Descriptions[name] = e.Description
	}
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		if err := ears.ValidateActor(name); err != nil {
			return fmt.Errorf("actors: %q: %w", name, err)
		}
		for _, alias := range entries[name].Aliases {
			if err := a.addAlias(alias, name); err != nil {
				return fmt.Errorf("actors: %q: alias %q: %w", name, alias, err)
			}
		}
	}
	return nil
}

// addAlias records alias as another name of the actor name.
func (a *Actors) addAlias(alias, name string) error {
	if err := ears.ValidateActor(alias); err != nil {
		return err
	}
	if _, isName := a.Descriptions[alias]; isName {
		return errors.New("is the name of an actor")
	}
	if other, taken := a.Aliases[alias]; taken {
		return fmt.Errorf("already stands for %q", other)
	}
	if a.Aliases == nil {
		a.Aliases = map[string]string{}
	}
	a.Aliases[alias] = name
	return nil
}
