package cosmoseed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cometbft/cometbft/v2/config"
	"github.com/cometbft/cometbft/v2/libs/log"
	"github.com/cometbft/cometbft/v2/p2p"
	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/cometbft/cometbft/v2/p2p/pex"
	"github.com/cometbft/cometbft/v2/p2p/transport/tcp"
	tcpconn "github.com/cometbft/cometbft/v2/p2p/transport/tcp/conn"
	"github.com/cometbft/cometbft/v2/version"

	"github.com/voluzi/cosmoseed/pkg/seedreactor"
)

type Seeder struct {
	home   string
	key    *p2p.NodeKey
	cfg    *Config
	logger log.Logger

	transport *tcp.MultiplexTransport
	book      p2p.AddrBook
	pex       *seedreactor.SeedReactor
	sw        *p2p.Switch

	httpServer *http.Server
}

func NewSeeder(home string, config *Config) (*Seeder, error) {
	logOpt, err := log.AllowLevel(config.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize log options: %w", err)
	}
	logger := log.NewFilter(log.NewLogger(os.Stdout), logOpt)

	nodeKeyPath := path.Join(home, config.NodeKeyFile)
	addrBookPath := path.Join(home, config.AddrBookFile)

	logger.Debug("cosmoseed",
		"version", Version,
		"node-key-file", nodeKeyPath,
		"address-book-file", addrBookPath,
		"chain-id", config.ChainID,
		"seeds", config.Seeds,
		"api-addr", config.ApiAddr,
		"p2p-addr", config.ListenAddr,
		"log-level", config.LogLevel,
		"allow-non-routable", config.AllowNonRoutable,
		"max-inbound", config.MaxInboundPeers,
		"max-outbound", config.MaxOutboundPeers,
		"max-packet-msg-payload-size", config.MaxPacketMsgPayloadSize,
		"dial-workers", config.DialWorkers,
		"peer-queue-size", config.PeerQueueSize,
		"external-address", config.ExternalAddress,
	)

	if err := ensurePath(nodeKeyPath); err != nil {
		return nil, err
	}

	nodeKey, err := p2p.LoadOrGenNodeKey(nodeKeyPath)
	if err != nil {
		return nil, err
	}

	// Transport
	p2pConfig := generateP2PConfig(home, config)
	transport := createTransport(nodeKey, p2pConfig)

	// Address book
	book := pex.NewAddrBook(addrBookPath, !config.AllowNonRoutable)
	book.SetLogger(logger)

	// PEX Reactor
	pexReactor := seedreactor.NewReactor(
		book,
		splitAndTrimEmpty(p2pConfig.Seeds, ",", " "),
		config.PeerQueueSize,
		config.DialWorkers,
		!config.AllowNonRoutable,
	)
	pexReactor.SetLogger(logger)

	// p2p switch
	sw := p2p.NewSwitch(p2pConfig, transport)
	sw.SetNodeKey(nodeKey)
	sw.SetLogger(logger)
	sw.SetAddrBook(book)
	sw.AddReactor("pex", pexReactor)
	nodeInfo := generateNodeInfo(nodeKey, config)
	sw.SetNodeInfo(nodeInfo)

	return &Seeder{
		home:      home,
		cfg:       config,
		logger:    logger,
		key:       nodeKey,
		transport: transport,
		book:      book,
		pex:       pexReactor,
		sw:        sw,
	}, nil
}

func (s *Seeder) Start() error {
	if err := s.cfg.Validate(); err != nil {
		return err
	}

	s.logger.Info("starting cosmoseed node",
		"version", Version,
		"key", s.key.ID(),
		"listen", s.cfg.ListenAddr,
		"chain-id", s.cfg.ChainID,
	)

	addr, err := na.NewFromString(na.IDAddrString(s.key.ID(), s.cfg.ListenAddr))
	if err != nil {
		return err
	}

	if err = s.transport.Listen(*addr); err != nil {
		return err
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		s.logger.Info("shutting down...")
		if err = s.Stop(); err != nil {
			panic(err)
		}
	}()

	if err = s.sw.Start(); err != nil {
		return err
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    s.cfg.ApiAddr,
		Handler: mux,
	}

	s.logger.Info("HTTP server starting", "addr", s.httpServer.Addr)
	if err = s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("HTTP server failed", "err", err)
	}
	return nil
}

func (s *Seeder) Stop() error {
	s.book.Save()
	if err := s.sw.Stop(); err != nil {
		return err
	}
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.logger.Info("shutting down HTTP server")
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Seeder) GetNodeID() string {
	return s.key.ID()
}

func (s *Seeder) GetP2pAddress() string {
	if s.cfg.ExternalAddress != "" {
		if parts := strings.Split(s.cfg.ExternalAddress, ":"); len(parts) == 2 {
			return parts[0]
		}
	}

	localIp, err := getLocalIP()
	if err == nil {
		return localIp
	}

	// If everything above fails just return a default
	return "0.0.0.0"
}

func (s *Seeder) GetP2pPort() int {
	if s.cfg.ExternalAddress != "" {
		if parts := strings.Split(s.cfg.ExternalAddress, ":"); len(parts) == 2 {
			port, err := strconv.Atoi(parts[1])
			if err == nil {
				return port
			}
		}
	}

	parts := strings.Split(s.cfg.ListenAddr, ":")
	if len(parts) > 1 {
		port, err := strconv.Atoi(parts[len(parts)-1])
		if err == nil {
			return port
		}
	}

	// If everything above fails just return default port
	return 26656
}

func (s *Seeder) GetFullAddress() string {
	return fmt.Sprintf("%s@%s:%d", s.GetNodeID(), s.GetP2pAddress(), s.GetP2pPort())
}

func generateP2PConfig(home string, cfg *Config) *config.P2PConfig {
	p2pConfig := config.DefaultP2PConfig()

	p2pConfig.AddrBook = path.Join(home, cfg.AddrBookFile)
	p2pConfig.AddrBookStrict = !cfg.AllowNonRoutable
	p2pConfig.Seeds = cfg.Seeds
	p2pConfig.ListenAddress = cfg.ListenAddr
	p2pConfig.AllowDuplicateIP = true
	p2pConfig.MaxNumInboundPeers = cfg.MaxInboundPeers
	p2pConfig.MaxNumOutboundPeers = cfg.MaxOutboundPeers
	p2pConfig.MaxPacketMsgPayloadSize = cfg.MaxPacketMsgPayloadSize
	p2pConfig.ExternalAddress = cfg.ExternalAddress

	return p2pConfig
}

func createTransport(key *p2p.NodeKey, p2pConfig *config.P2PConfig) *tcp.MultiplexTransport {
	tcpConfig := tcpconn.DefaultMConnConfig()
	tcpConfig.FlushThrottle = p2pConfig.FlushThrottleTimeout
	tcpConfig.SendRate = p2pConfig.SendRate
	tcpConfig.RecvRate = p2pConfig.RecvRate
	tcpConfig.MaxPacketMsgPayloadSize = p2pConfig.MaxPacketMsgPayloadSize
	tcpConfig.TestFuzz = p2pConfig.TestFuzz
	tcpConfig.TestFuzzConfig = p2pConfig.TestFuzzConfig

	transport := tcp.NewMultiplexTransport(*key, tcpConfig)
	tcp.MultiplexTransportMaxIncomingConnections(p2pConfig.MaxNumInboundPeers)(transport)
	return transport
}

func generateNodeInfo(key *p2p.NodeKey, cfg *Config) p2p.NodeInfoDefault {
	return p2p.NodeInfoDefault{
		ProtocolVersion: p2p.ProtocolVersion{
			P2P:   version.P2PProtocol,
			Block: version.BlockProtocol,
		},
		DefaultNodeID: key.ID(),
		Network:       cfg.ChainID,
		Version:       version.CMTSemVer,
		Channels:      []byte{pex.PexChannel},
		ListenAddr:    cfg.ListenAddr,
		Moniker:       "cosmoseed",
	}
}
