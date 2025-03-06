package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type ClientConfig struct {
	ListenerConfig   `yaml:",inline"`
	ProxyConfig      `yaml:",inline"`
	TargetAddress    string            `yaml:"target-address"`
	WSUrl            string            `yaml:"ws-url"`
	WSHeaders        map[string]string `yaml:"ws-headers"`
	V2rayHttpUpgrade bool              `yaml:"v2ray-http-upgrade"`
	SkipCertVerify   bool              `yaml:"skip-cert-verify"`
	ServerName       string            `yaml:"servername"`
	ServerWSPath     string            `yaml:"server-ws-path"`
	Mtp              string            `yaml:"mtp"`
}

type ServerConfig struct {
	ListenerConfig `yaml:",inline"`
	ProxyConfig    `yaml:",inline"`
	Target         []ServerTargetConfig `yaml:"target"`
}

type UdpConfig struct {
	ListenerConfig `yaml:",inline"`
	TargetAddress  string  `yaml:"target-address"`
	Reserved       []uint8 `yaml:"reserved"`
}

type ListenerConfig struct {
	BindAddress    string `yaml:"bind-address"`
	FallbackConfig `yaml:",inline"`
	MMsg           bool `yaml:"mmsg"`
}

type FallbackConfig struct {
	SshFallbackAddress     string `yaml:"ssh-fallback-address"`
	SshFallbackTimeout     int    `yaml:"ssh-fallback-timeout"`
	TLSFallbackAddress     string `yaml:"tls-fallback-address"` // old compatibility
	WSFallbackAddress      string `yaml:"ws-fallback-address"`
	UnknownFallbackAddress string `yaml:"unknown-fallback-address"`

	TLSFallback    []TLSFallbackConfig   `yaml:"tls-fallback"`
	QuicFallback   []QuicFallbackConfig  `yaml:"quic-fallback"`
	SSFallback     []SSFallbackConfig    `yaml:"ss-fallback"`
	SS2022Fallback []SSFallbackConfig    `yaml:"ss2022-fallback"`
	VmessFallback  []VmessFallbackConfig `yaml:"vmess-fallback"`
}

type TLSFallbackConfig struct {
	SNI     string `yaml:"sni"`
	Address string `yaml:"address"`
	Mtp     string `yaml:"mtp"`
}

type QuicFallbackConfig struct {
	SNI     string `yaml:"sni"`
	Address string `yaml:"address"`
}

type SSFallbackConfig struct {
	Name     string `yaml:"name"`
	Method   string `yaml:"method"`
	Password string `yaml:"password"`
	Address  string `yaml:"address"`
}

type VmessFallbackConfig struct {
	Name    string `yaml:"name"`
	UUID    string `yaml:"uuid"`
	Address string `yaml:"address"`
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
	UdpConfigs    []UdpConfig    `yaml:"udp"`
	DisableServer bool           `yaml:"disable-server"`
	DisableClient bool           `yaml:"disable-client"`
	DisableUdp    bool           `yaml:"disable-udp"`
	DisableLog    bool           `yaml:"disable-log"`
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
		UdpConfigs:    []UdpConfig{},
		DisableServer: false,
		DisableClient: false,
		DisableUdp:    false,
		DisableLog:    false,
	}
	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
