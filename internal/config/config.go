// Package config loads mikroview's configuration with precedence
// defaults < YAML file < environment variables < CLI flags.
//
// The list of monitored RouterOS devices lives only in the YAML file: it's
// a structured list of records, which doesn't map cleanly onto env vars
// without an awkward numbered-key scheme. Scalar runtime knobs (ports,
// retention, log level) stay overridable via env vars for simple
// single-container deployments that don't want to mount a file at all.
package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Device struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	SourceIP string `yaml:"sourceIp"`
}

type Listen struct {
	SyslogUDP string `yaml:"syslogUdp"`
	SyslogTCP string `yaml:"syslogTcp"`
	HTTP      string `yaml:"http"`
}

type Store struct {
	Retention time.Duration `yaml:"retention"`
	MaxEvents int           `yaml:"maxEvents"`
}

type Config struct {
	Listen  Listen   `yaml:"listen"`
	Store   Store    `yaml:"store"`
	Devices []Device `yaml:"devices"`
}

func defaults() Config {
	return Config{
		Listen: Listen{
			SyslogUDP: ":1514",
			SyslogTCP: ":1514",
			HTTP:      ":8080",
		},
		Store: Store{
			Retention: 24 * time.Hour,
			MaxEvents: 200_000,
		},
	}
}

// Load builds the effective Config from defaults, an optional YAML file,
// environment variables, and CLI flags (in that order of increasing
// precedence). configPath and args are taken as parameters (rather than
// read from os.Args/env directly) so tests can call this deterministically.
func Load(configPath string, args []string) (Config, error) {
	cfg := defaults()

	if configPath != "" {
		if err := loadYAML(configPath, &cfg); err != nil {
			return Config{}, fmt.Errorf("loading config file %s: %w", configPath, err)
		}
	}

	applyEnv(&cfg)

	if err := applyFlags(&cfg, args); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func loadYAML(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	return dec.Decode(cfg)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("MIKROVIEW_LISTEN_SYSLOG_UDP"); v != "" {
		cfg.Listen.SyslogUDP = v
	}
	if v := os.Getenv("MIKROVIEW_LISTEN_SYSLOG_TCP"); v != "" {
		cfg.Listen.SyslogTCP = v
	}
	if v := os.Getenv("MIKROVIEW_LISTEN_HTTP"); v != "" {
		cfg.Listen.HTTP = v
	}
	if v := os.Getenv("MIKROVIEW_STORE_RETENTION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Store.Retention = d
		}
	}
	if v := os.Getenv("MIKROVIEW_STORE_MAX_EVENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Store.MaxEvents = n
		}
	}
}

func applyFlags(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("mikroview", flag.ContinueOnError)
	syslogUDP := fs.String("syslog-udp", cfg.Listen.SyslogUDP, "syslog UDP listen address")
	syslogTCP := fs.String("syslog-tcp", cfg.Listen.SyslogTCP, "syslog TCP listen address")
	httpAddr := fs.String("http", cfg.Listen.HTTP, "HTTP listen address")
	retention := fs.Duration("retention", cfg.Store.Retention, "event retention window")
	maxEvents := fs.Int("max-events", cfg.Store.MaxEvents, "max events held in the ring buffer")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Listen.SyslogUDP = *syslogUDP
	cfg.Listen.SyslogTCP = *syslogTCP
	cfg.Listen.HTTP = *httpAddr
	cfg.Store.Retention = *retention
	cfg.Store.MaxEvents = *maxEvents
	return nil
}
