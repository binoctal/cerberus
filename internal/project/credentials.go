package project

import (
	"fmt"
	"os"
	"strings"
)

func ResolveCredentials(cfg *Config) *Config {
	result := *cfg
	result.Actors = make([]Actor, len(cfg.Actors))
	for i, a := range cfg.Actors {
		actor := a
		envPrefix := "CERBERUS_ACTOR_" + strings.ToUpper(strings.ReplaceAll(a.Name, "-", "_"))
		if email := os.Getenv(envPrefix + "_EMAIL"); email != "" {
			actor.Credentials.Email = email
		}
		if pass := os.Getenv(envPrefix + "_PASSWORD"); pass != "" {
			actor.Credentials.Password = pass
		}
		if token := os.Getenv(envPrefix + "_TOKEN"); token != "" {
			actor.Credentials.Token = token
		}
		result.Actors[i] = actor
	}
	return &result
}

func ResolveActorCredentials(cfg *Config, actorName string) (email, password string, err error) {
	resolved := ResolveCredentials(cfg)
	for _, a := range resolved.Actors {
		if a.Name == actorName {
			return a.Credentials.Email, a.Credentials.Password, nil
		}
	}
	return "", "", fmt.Errorf("actor %q not found in config", actorName)
}
