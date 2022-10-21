package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/wwqgtxx/wstunnel/client"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/server"
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
	buf, err := config.ReadConfig(configFile)
	if err != nil {
		panic(err)
	}
	cfg, err := config.ParseConfig(buf)
	if err != nil {
		panic(err)
	}
	for _, clientConfig := range cfg.ClientConfigs {
		client.BuildClient(clientConfig)
	}
	for _, serverConfig := range cfg.ServerConfigs {
		server.BuildServer(serverConfig)
	}
	if !cfg.DisableClient {
		client.StartClients()
	}
	if !cfg.DisableServer {
		server.StartServers()
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
