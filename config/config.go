package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type ClientConfig struct {
	ListenerConfig `yaml:",inline"`
	ProxyConfig    `yaml:",inline"`
	TargetAddress  string            `yaml:"target-address"`
	WSUrl          string            `yaml:"ws-url"`
	WSHeaders      map[string]string `yaml:"ws-headers"`
	SkipCertVerify bool              `yaml:"skip-cert-verify"`
	ServerName     string            `yaml:"servername"`
	ServerWSPath   string            `yaml:"server-ws-path"`
}

type ServerConfig struct {
	ListenerConfig `yaml:",inline"`
	ProxyConfig    `yaml:",inline"`
	Target         []ServerTargetConfig `yaml:"target"`
}

type ListenerConfig struct {
	BindAddress            string `yaml:"bind-address"`
	SshFallbackAddress     string `yaml:"ssh-fallback-address"`
	SshFallbackTimeout     int    `yaml:"ssh-fallback-timeout"`
	TLSFallbackAddress     string `yaml:"tls-fallback-address"`
	WSFallbackAddress      string `yaml:"ws-fallback-address"`
	UnknownFallbackAddress string `yaml:"unknown-fallback-address"`
}

type ProxyConfig struct {
	Proxy string `yaml:"proxy"`
}

type ServerTargetConfig struct {
	*ProxyConfig  `yaml:",inline"`
	TargetAddress string `yaml:"target-address"`
	WSPath        string `yaml:"ws-path"`
}

type Config struct {
	ServerConfigs []ServerConfig `yaml:"server"`
	ClientConfigs []ClientConfig `yaml:"client"`
	DisableServer bool           `yaml:"disable-server"`
	DisableClient bool           `yaml:"disable-client"`
}

func ReadConfig(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("Configuration file %s is empty", path)
	}

	return data, err
}

func ParseConfig(buf []byte) (*Config, error) {
	cfg := &Config{
		ServerConfigs: []ServerConfig{},
		ClientConfigs: []ClientConfig{},
		DisableServer: false,
		DisableClient: false,
	}
	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}