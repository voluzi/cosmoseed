package cosmoseed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	book      pex.AddrBook
	pex       *seedreactor.SeedReactor
	sw        *p2p.Switch

	httpServer         *http.Server
	metricsServer      *http.Server
	stopOnce           sync.Once
	transportCloseOnce sync.Once
	stopCh             chan struct{}
	stopErr            error
	running            atomic.Bool
	mu                 sync.Mutex
	startMu            sync.Mutex
	startFailed        bool
}

func NewSeeder(home string, config *Config) (*Seeder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
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
		config.VerificationTTL,
		config.RecheckInterval,
		config.MaxDialFailures,
		config.BanDuration,
	)
	pexReactor.SetLogger(logger)

	// p2p switch
	sw := p2p.NewSwitch(p2pConfig, transport)
	sw.SetNodeKey(nodeKey)
	sw.SetLogger(logger)
	sw.SetAddrBook(pexReactor.ServingBook())
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
		stopCh:    make(chan struct{}),
	}, nil
}

func (s *Seeder) Start() error {
	return s.Run(context.Background())
}

func (s *Seeder) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.startMu.Lock()
	s.mu.Lock()
	if s.running.Load() {
		s.mu.Unlock()
		s.startMu.Unlock()
		return errors.New("seeder already running")
	}
	select {
	case <-s.stopCh:
		s.mu.Unlock()
		s.startMu.Unlock()
		return errors.New("seeder already stopped")
	default:
	}
	if s.startFailed {
		s.mu.Unlock()
		s.startMu.Unlock()
		return errors.New("seeder already stopped")
	}
	s.mu.Unlock()
	startupFinished := false
	defer func() {
		if !startupFinished {
			s.startFailed = true
			s.startMu.Unlock()
			_ = s.Stop()
		}
	}()

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

	apiListener, err := net.Listen("tcp", s.cfg.ApiAddr)
	if err != nil {
		return fmt.Errorf("listen API: %w", err)
	}
	var metricsListener net.Listener
	if s.cfg.MetricsAddr != "" {
		metricsListener, err = net.Listen("tcp", s.cfg.MetricsAddr)
		if err != nil {
			_ = apiListener.Close()
			return fmt.Errorf("listen metrics: %w", err)
		}
	}
	if err = s.transport.Listen(*addr); err != nil {
		_ = apiListener.Close()
		if metricsListener != nil {
			_ = metricsListener.Close()
		}
		return err
	}
	if err = s.sw.Start(); err != nil {
		_ = apiListener.Close()
		if metricsListener != nil {
			_ = metricsListener.Close()
		}
		return err
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.mu.Lock()
	s.httpServer = &http.Server{
		Addr:              s.cfg.ApiAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if metricsListener != nil {
		s.metricsServer = &http.Server{Addr: s.cfg.MetricsAddr, Handler: s.metricsHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	}
	s.running.Store(true)
	s.mu.Unlock()
	startupFinished = true
	s.startMu.Unlock()
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- s.httpServer.Serve(apiListener) }()
	if metricsListener != nil {
		go func() { errorsCh <- s.metricsServer.Serve(metricsListener) }()
	}
	select {
	case <-ctx.Done():
	case <-s.stopCh:
	case err = <-errorsCh:
	}
	stopErr := s.Stop()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve listener: %w", err)
	}
	return stopErr
}

func (s *Seeder) Stop() error {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.startMu.Lock()
		defer s.startMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.mu.Lock()
		api, metrics := s.httpServer, s.metricsServer
		s.mu.Unlock()
		if api != nil {
			if err := api.Shutdown(ctx); err != nil {
				s.stopErr = errors.Join(s.stopErr, err)
			}
		}
		if metrics != nil {
			if err := metrics.Shutdown(ctx); err != nil {
				s.stopErr = errors.Join(s.stopErr, err)
			}
		}
		if s.sw.IsRunning() {
			if err := s.sw.Stop(); err != nil {
				s.stopErr = errors.Join(s.stopErr, err)
			}
		} else if s.pex.IsRunning() {
			if err := s.pex.Stop(); err != nil {
				s.stopErr = errors.Join(s.stopErr, err)
			}
		} else if s.book.IsRunning() {
			if err := s.book.Stop(); err != nil {
				s.stopErr = errors.Join(s.stopErr, err)
			}
		}
		s.transportCloseOnce.Do(func() { s.stopErr = errors.Join(s.stopErr, s.transport.Close()) })
		s.running.Store(false)
	})
	return s.stopErr
}

func (s *Seeder) GetNodeID() string {
	return s.key.ID()
}

func (s *Seeder) GetP2pAddress() string {
	if s.cfg.ExternalAddress != "" {
		host, _, err := net.SplitHostPort(s.cfg.ExternalAddress)
		if err == nil {
			return host
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
		_, port, err := net.SplitHostPort(s.cfg.ExternalAddress)
		if err == nil {
			parsed, _ := strconv.Atoi(port)
			return parsed
		}
	}
	_, port, err := net.SplitHostPort(strings.TrimPrefix(s.cfg.ListenAddr, "tcp://"))
	if err == nil {
		parsed, _ := strconv.Atoi(port)
		return parsed
	}

	// If everything above fails just return default port
	return 26656
}

func (s *Seeder) GetFullAddress() string {
	return fmt.Sprintf("%s@%s", s.GetNodeID(), net.JoinHostPort(s.GetP2pAddress(), strconv.Itoa(s.GetP2pPort())))
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
