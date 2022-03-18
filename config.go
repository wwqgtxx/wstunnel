package main

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
)

type ClientConfig struct {
	BindAddress    string            `yaml:"bind-address"`
	TargetAddress  string            `yaml:"target-address"`
	WSUrl          string            `yaml:"ws-url"`
	WSHeaders      map[string]string `yaml:"ws-headers"`
	SkipCertVerify bool              `yaml:"skip-cert-verify"`
	ServerName     string            `yaml:"servername"`
	Proxy          string            `yaml:"proxy"`
	ServerWSPath   string            `yaml:"server-ws-path"`
}

type ServerConfig struct {
	BindAddress string               `yaml:"bind-address"`
	Target      []ServerTargetConfig `yaml:"target"`
}

type ServerTargetConfig struct {
	TargetAddress string `yaml:"target-address"`
	WSPath        string `yaml:"ws-path"`
}

type Config struct {
	ServerConfigs []ServerConfig `yaml:"server"`
	ClientConfigs []ClientConfig `yaml:"client"`
}

func readConfig(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, err
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("Configuration file %s is empty", path)
	}

	return data, err
}

func parseConfig(buf []byte) (*Config, error) {
	cfg := &Config{
		ServerConfigs: []ServerConfig{},
		ClientConfigs: []ClientConfig{},
	}
	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
