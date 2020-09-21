package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/angopher/chronus/raftmeta"
	imeta "github.com/angopher/chronus/services/meta"
	"github.com/angopher/chronus/x"
	"github.com/influxdata/influxdb/logger"
	"github.com/influxdata/influxdb/services/meta"
	"go.uber.org/zap"
	"gopkg.in/natefinch/lumberjack.v2"
)

func initialLogging(config *raftmeta.Config) (*zap.Logger, error) {
	cfg := logger.NewConfig()
	switch strings.ToLower(config.LogLevel) {
	case "info":
		cfg.Level = zap.InfoLevel
	case "warn", "warning":
		cfg.Level = zap.WarnLevel
	case "debug":
		cfg.Level = zap.DebugLevel
	case "fatal":
		cfg.Level = zap.FatalLevel
	case "panic":
		cfg.Level = zap.PanicLevel
	}
	if config.LogDir != "" {
		dir := strings.TrimRight(config.LogDir, string(filepath.Separator))
		return cfg.New(&lumberjack.Logger{
			Filename:   filepath.Join(dir, "metad.log"),
			MaxSize:    100,
			MaxBackups: 5,
			Compress:   true,
		})
	} else {
		return cfg.New(os.Stderr)
	}
}

func main() {
	f := flag.NewFlagSet("metad", flag.ExitOnError)
	configFile := f.String("config", "", "Specify config file")
	f.Parse(os.Args[1:])

	config := raftmeta.NewConfig()
	if *configFile != "" {
		x.Check((&config).FromTomlFile(*configFile))
	} else {
		fmt.Print("Sample configuration:\n\n")
		toml.NewEncoder(os.Stdout).Encode(&config)
		fmt.Println()
		f.Usage()
		return
	}

	fmt.Printf("config:%+v\n", config)

	metaCli := imeta.NewClient(&meta.Config{
		RetentionAutoCreate: config.RetentionAutoCreate,
		LoggingEnabled:      true,
	})
	log, err := initialLogging(&config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error to initialize logging", err)
		return
	}

	metaCli.WithLogger(log)
	err = metaCli.Open()
	x.Check(err)

	node := raftmeta.NewRaftNode(config)
	node.MetaCli = metaCli
	node.WithLogger(log)

	t := raftmeta.NewTransport()
	t.WithLogger(log)

	node.Transport = t
	node.InitAndStartNode()
	go node.Run()

	//线性一致性读
	linearRead := raftmeta.NewLinearizabler(node)
	go linearRead.ReadLoop()

	service := raftmeta.NewMetaService(config.MyAddr, metaCli, node, linearRead)
	service.InitRouter()
	service.WithLogger(log)
	service.Start()
}
