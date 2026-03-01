package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig `yaml:"server"`
	Repositories []RepoConfig `yaml:"repositories"`
}

type ServerConfig struct {
	Port                int    `yaml:"port"`
	DBPath              string `yaml:"db_path"`
	RefreshConcurrency  int    `yaml:"refresh_concurrency"`
}

type RepoConfig struct {
	Name          string `yaml:"name"`
	Path          string `yaml:"path"`
	Remote        string `yaml:"remote"`
	DefaultBranch string `yaml:"default_branch"`
	MakeTarget    string `yaml:"make_target"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	setDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.DBPath == "" {
		cfg.Server.DBPath = "./build-server.db"
	}
	if cfg.Server.RefreshConcurrency == 0 {
		cfg.Server.RefreshConcurrency = 4
	}
	for i := range cfg.Repositories {
		r := &cfg.Repositories[i]
		if r.Remote == "" {
			r.Remote = "origin"
		}
		if r.DefaultBranch == "" {
			r.DefaultBranch = "main"
		}
		if r.MakeTarget == "" {
			r.MakeTarget = "deploy"
		}
	}
}

func validate(cfg *Config) error {
	names := make(map[string]bool)
	for _, r := range cfg.Repositories {
		if r.Name == "" {
			return fmt.Errorf("repository missing name")
		}
		if r.Path == "" {
			return fmt.Errorf("repository %q missing path", r.Name)
		}
		if names[r.Name] {
			return fmt.Errorf("duplicate repository name: %q", r.Name)
		}
		names[r.Name] = true
	}
	return nil
}
