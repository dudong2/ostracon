package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/line/ostracon/abci/server"
	"github.com/line/ostracon/config"
	"github.com/line/ostracon/crypto/ed25519"
	"github.com/line/ostracon/libs/log"
	tmnet "github.com/line/ostracon/libs/net"
	"github.com/line/ostracon/light"
	lproxy "github.com/line/ostracon/light/proxy"
	lrpc "github.com/line/ostracon/light/rpc"
	dbs "github.com/line/ostracon/light/store/db"
	"github.com/line/ostracon/node"
	"github.com/line/ostracon/p2p"
	"github.com/line/ostracon/privval"
	"github.com/line/ostracon/proxy"
	rpcserver "github.com/line/ostracon/rpc/jsonrpc/server"
	"github.com/line/ostracon/test/e2e/app"
	e2e "github.com/line/ostracon/test/e2e/pkg"
)

var logger = log.NewOCLogger(log.NewSyncWriter(os.Stdout))

// main is the binary entrypoint.
func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %v <configfile>", os.Args[0])
		return
	}
	configFile := ""
	if len(os.Args) == 2 {
		configFile = os.Args[1]
	}

	if err := run(configFile); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

// run runs the application - basically like main() with error handling.
func run(configFile string) error {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return err
	}

	// Start remote signer (must start before node if running builtin).
	if cfg.PrivValServer != "" {
		if err = startSigner(cfg); err != nil {
			return err
		}
		if cfg.Protocol == "builtin" {
			time.Sleep(1 * time.Second)
		}
	}

	// Start app server.
	switch cfg.Protocol {
	case "socket", "grpc":
		err = startApp(cfg)
	case "builtin":
		if cfg.Mode == string(e2e.ModeLight) {
			err = startLightClient(cfg)
		} else {
			err = startNode(cfg)
		}
	default:
		err = fmt.Errorf("invalid protocol %q", cfg.Protocol)
	}
	if err != nil {
		return err
	}

	// Apparently there's no way to wait for the server, so we just sleep
	for {
		time.Sleep(1 * time.Hour)
	}
}

// startApp starts the application server, listening for connections from Ostracon.
func startApp(cfg *Config) error {
	app, err := app.NewApplication(cfg.App())
	if err != nil {
		return err
	}
	server, err := server.NewServer(cfg.Listen, cfg.Protocol, app)
	if err != nil {
		return err
	}
	err = server.Start()
	if err != nil {
		return err
	}
	logger.Info("start app", "msg", log.NewLazySprintf("Server listening on %v (%v protocol)", cfg.Listen, cfg.Protocol))
	return nil
}

// startNode starts an Ostracon node running the application directly. It assumes the Ostracon
// configuration is in $OCHOME/config/ostracon.toml.
//
// FIXME There is no way to simply load the configuration from a file, so we need to pull in Viper.
func startNode(cfg *Config) error {
	app, err := app.NewApplication(cfg.App())
	if err != nil {
		return err
	}

	tmcfg, nodeLogger, nodeKey, err := setupNode()
	if err != nil {
		return fmt.Errorf("failed to setup config: %w", err)
	}

	privVal := privval.LoadOrGenFilePV(tmcfg.PrivValidatorKeyFile(), tmcfg.PrivValidatorStateFile())
	if err != nil {
		return fmt.Errorf("failed to load/generate FilePV%w", err)
	}
	n, err := node.NewNode(tmcfg,
		privVal,
		nodeKey,
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(tmcfg),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(tmcfg.Instrumentation),
		nodeLogger,
	)
	if err != nil {
		return err
	}
	return n.Start()
}

func startLightClient(cfg *Config) error {
	tmcfg, nodeLogger, _, err := setupNode()
	if err != nil {
		return err
	}

	dbContext := &node.DBContext{ID: "light", Config: tmcfg}
	lightDB, err := node.DefaultDBProvider(dbContext)
	if err != nil {
		return err
	}

	providers := rpcEndpoints(tmcfg.P2P.PersistentPeers)

	c, err := light.NewHTTPClient(
		context.Background(),
		cfg.ChainID,
		light.TrustOptions{
			Period: tmcfg.StateSync.TrustPeriod,
			Height: tmcfg.StateSync.TrustHeight,
			Hash:   tmcfg.StateSync.TrustHashBytes(),
		},
		providers[0],
		providers[1:],
		dbs.New(lightDB, "light"),
		light.Logger(nodeLogger),
	)
	if err != nil {
		return err
	}

	rpccfg := rpcserver.DefaultConfig()
	rpccfg.MaxBodyBytes = tmcfg.RPC.MaxBodyBytes
	rpccfg.MaxHeaderBytes = tmcfg.RPC.MaxHeaderBytes
	rpccfg.MaxOpenConnections = tmcfg.RPC.MaxOpenConnections
	// If necessary adjust global WriteTimeout to ensure it's greater than
	// TimeoutBroadcastTxCommit.
	// See https://github.com/tendermint/tendermint/issues/3435
	if rpccfg.WriteTimeout <= tmcfg.RPC.TimeoutBroadcastTxCommit {
		rpccfg.WriteTimeout = tmcfg.RPC.TimeoutBroadcastTxCommit + 1*time.Second
	}

	p, err := lproxy.NewProxy(c, tmcfg.RPC.ListenAddress, providers[0], rpccfg, nodeLogger,
		lrpc.KeyPathFn(lrpc.DefaultMerkleKeyPathFn()))
	if err != nil {
		return err
	}

	logger.Info("Starting proxy...", "laddr", tmcfg.RPC.ListenAddress)
	if err := p.ListenAndServe(); err != http.ErrServerClosed {
		// Error starting or closing listener:
		logger.Error("proxy ListenAndServe", "err", err)
	}

	return nil
}

// startSigner starts a signer server connecting to the given endpoint.
func startSigner(cfg *Config) error {
	filePV := privval.LoadFilePV(cfg.PrivValKey, cfg.PrivValState)

	protocol, address := tmnet.ProtocolAndAddress(cfg.PrivValServer)
	var dialFn privval.SocketDialer
	switch protocol {
	case "tcp":
		dialFn = privval.DialTCPFn(address, 3*time.Second, ed25519.GenPrivKey())
	case "unix":
		dialFn = privval.DialUnixFn(address)
	default:
		return fmt.Errorf("invalid privval protocol %q", protocol)
	}

	endpoint := privval.NewSignerDialerEndpoint(logger, dialFn,
		privval.SignerDialerEndpointRetryWaitInterval(1*time.Second),
		privval.SignerDialerEndpointConnRetries(100))
	err := privval.NewSignerServer(endpoint, cfg.ChainID, filePV).Start()
	if err != nil {
		return err
	}
	logger.Info("start signer", "msg", log.NewLazySprintf("Remote signer connecting to %v", cfg.PrivValServer))
	return nil
}

func setupNode() (*config.Config, log.Logger, *p2p.NodeKey, error) {
	var tmcfg *config.Config

	home := os.Getenv("OCHOME")
	if home == "" {
		return nil, nil, nil, errors.New("OCHOME not set")
	}

	viper.AddConfigPath(filepath.Join(home, "config"))
	viper.SetConfigName("config")

	if err := viper.ReadInConfig(); err != nil {
		return nil, nil, nil, err
	}

	tmcfg = config.DefaultConfig()

	if err := viper.Unmarshal(tmcfg); err != nil {
		return nil, nil, nil, err
	}

	tmcfg.SetRoot(home)

	if err := tmcfg.ValidateBasic(); err != nil {
		return nil, nil, nil, fmt.Errorf("error in config file: %w", err)
	}

	if tmcfg.LogFormat == config.LogFormatJSON {
		logger = log.NewOCJSONLogger(log.NewSyncWriter(os.Stdout))
	}

	nodeLogger, err := log.ParseLogLevel(tmcfg.LogLevel, logger, config.DefaultLogLevel)
	if err != nil {
		return nil, nil, nil, err
	}

	nodeLogger = nodeLogger.With("module", "main")

	nodeKey, err := p2p.LoadOrGenNodeKey(tmcfg.NodeKeyFile())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load or gen node key %s: %w", tmcfg.NodeKeyFile(), err)
	}

	return tmcfg, nodeLogger, nodeKey, nil
}

// rpcEndpoints takes a list of persistent peers and splits them into a list of rpc endpoints
// using 26657 as the port number
func rpcEndpoints(peers string) []string {
	arr := strings.Split(peers, ",")
	endpoints := make([]string, len(arr))
	for i, v := range arr {
		urlString := strings.SplitAfter(v, "@")[1]
		hostName := strings.Split(urlString, ":26656")[0]
		// use RPC port instead
		port := 26657
		rpcEndpoint := "http://" + hostName + ":" + fmt.Sprint(port)
		endpoints[i] = rpcEndpoint
	}
	return endpoints
}
