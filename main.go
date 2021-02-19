package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
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
		BuildClient(clientConfig)
	}
	for _, serverConfig := range cfg.ServerConfigs {
		BuildServer(serverConfig)
	}
	StartClients()
	StartServers()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
