package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	configFile := "config.yaml"
	if len(os.Args) == 2 {
		configFile = os.Args[1]
	}
	if !filepath.IsAbs(configFile) {
		currentDir, _ := os.Getwd()
		configFile = filepath.Join(currentDir, configFile)
	}
	buf, err := readConfig(configFile)
	if err != nil {
		panic(err)
	}
	cfg, err := parseConfig(buf)
	if err != nil {
		panic(err)
	}
	for _, clientConfig := range cfg.ClientConfigs {
		StartClient(clientConfig)
	}
	for _, serverConfig := range cfg.ServerConfigs {
		StartServer(serverConfig)
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
