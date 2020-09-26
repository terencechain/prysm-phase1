package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto"
	fssz "github.com/ferranbt/fastssz"
	ptypes "github.com/gogo/protobuf/types"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/event"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/grpcutils"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/wallet"
	"github.com/prysmaticlabs/prysm/validator/db"
	keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v1"
	v2 "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
	slashingprotection "github.com/prysmaticlabs/prysm/validator/slashing-protection"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

var log = logrus.WithField("prefix", "validator")

// SyncChecker is able to determine if a beacon node is currently
// going through chain synchronization.
type SyncChecker interface {
	Syncing(ctx context.Context) (bool, error)
}

// GenesisFetcher can retrieve genesis information such as
// the genesis time and the validator deposit contract address.
type GenesisFetcher interface {
	GenesisInfo(ctx context.Context) (*ethpb.Genesis, error)
}

// ValidatorService represents a service to manage the validator client
// routine.
type ValidatorService struct {
	useWeb                bool
	emitAccountMetrics    bool
	logValidatorBalances  bool
	conn                  *grpc.ClientConn
	grpcRetryDelay        time.Duration
	grpcRetries           uint
	maxCallRecvMsgSize    int
	walletInitializedFeed *event.Feed
	cancel                context.CancelFunc
	db                    db.Database
	keyManager            keymanager.KeyManager
	dataDir               string
	withCert              string
	endpoint              string
	validator             Validator
	protector             slashingprotection.Protector
	ctx                   context.Context
	keyManagerV2          v2.IKeymanager
	grpcHeaders           []string
	graffiti              []byte
}

// Config for the validator service.
type Config struct {
	UseWeb                     bool
	LogValidatorBalances       bool
	EmitAccountMetrics         bool
	WalletInitializedFeed      *event.Feed
	GrpcRetriesFlag            uint
	GrpcRetryDelay             time.Duration
	GrpcMaxCallRecvMsgSizeFlag int
	Protector                  slashingprotection.Protector
	Endpoint                   string
	Validator                  Validator
	ValDB                      db.Database
	KeyManagerV2               v2.IKeymanager
	KeyManager                 keymanager.KeyManager
	GraffitiFlag               string
	CertFlag                   string
	DataDir                    string
	GrpcHeadersFlag            string
}

// NewValidatorService creates a new validator service for the service
// registry.
func NewValidatorService(ctx context.Context, cfg *Config) (*ValidatorService, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &ValidatorService{
		ctx:                   ctx,
		cancel:                cancel,
		endpoint:              cfg.Endpoint,
		withCert:              cfg.CertFlag,
		dataDir:               cfg.DataDir,
		graffiti:              []byte(cfg.GraffitiFlag),
		keyManager:            cfg.KeyManager,
		keyManagerV2:          cfg.KeyManagerV2,
		logValidatorBalances:  cfg.LogValidatorBalances,
		emitAccountMetrics:    cfg.EmitAccountMetrics,
		maxCallRecvMsgSize:    cfg.GrpcMaxCallRecvMsgSizeFlag,
		grpcRetries:           cfg.GrpcRetriesFlag,
		grpcRetryDelay:        cfg.GrpcRetryDelay,
		grpcHeaders:           strings.Split(cfg.GrpcHeadersFlag, ","),
		protector:             cfg.Protector,
		validator:             cfg.Validator,
		db:                    cfg.ValDB,
		walletInitializedFeed: cfg.WalletInitializedFeed,
		useWeb:                cfg.UseWeb,
	}, nil
}

// Start the validator service. Launches the main go routine for the validator
// client.
func (v *ValidatorService) Start() {
	streamInterceptor := grpc.WithStreamInterceptor(middleware.ChainStreamClient(
		grpc_opentracing.StreamClientInterceptor(),
		grpc_prometheus.StreamClientInterceptor,
		grpc_retry.StreamClientInterceptor(),
	))
	dialOpts := ConstructDialOptions(
		v.maxCallRecvMsgSize,
		v.withCert,
		v.grpcHeaders,
		v.grpcRetries,
		v.grpcRetryDelay,
		streamInterceptor,
	)
	if dialOpts == nil {
		return
	}
	conn, err := grpc.DialContext(v.ctx, v.endpoint, dialOpts...)
	if err != nil {
		log.Errorf("Could not dial endpoint: %s, %v", v.endpoint, err)
		return
	}
	if v.withCert != "" {
		log.Info("Established secure gRPC connection")
	}

	v.conn = conn
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1920, // number of keys to track.
		MaxCost:     192,  // maximum cost of cache, 1 item = 1 cost.
		BufferItems: 64,   // number of keys per Get buffer.
	})
	if err != nil {
		panic(err)
	}

	aggregatedSlotCommitteeIDCache, err := lru.New(int(params.BeaconConfig().MaxCommitteesPerSlot))
	if err != nil {
		log.Errorf("Could not initialize cache: %v", err)
		return
	}

	v.validator = &validator{
		db:                             v.db,
		validatorClient:                ethpb.NewBeaconNodeValidatorClient(v.conn),
		beaconClient:                   ethpb.NewBeaconChainClient(v.conn),
		node:                           ethpb.NewNodeClient(v.conn),
		keyManager:                     v.keyManager,
		keyManagerV2:                   v.keyManagerV2,
		graffiti:                       v.graffiti,
		logValidatorBalances:           v.logValidatorBalances,
		emitAccountMetrics:             v.emitAccountMetrics,
		startBalances:                  make(map[[48]byte]uint64),
		prevBalance:                    make(map[[48]byte]uint64),
		attLogs:                        make(map[[32]byte]*attSubmitted),
		domainDataCache:                cache,
		aggregatedSlotCommitteeIDCache: aggregatedSlotCommitteeIDCache,
		protector:                      v.protector,
		voteStats:                      voteStats{startEpoch: ^uint64(0)},
		useWeb:                         v.useWeb,
		walletInitializedFeed:          v.walletInitializedFeed,
	}
	go run(v.ctx, v.validator)
	go v.recheckKeys(v.ctx)
}

// Stop the validator service.
func (v *ValidatorService) Stop() error {
	v.cancel()
	log.Info("Stopping service")
	if v.conn != nil {
		return v.conn.Close()
	}
	return nil
}

// Status of the validator service.
func (v *ValidatorService) Status() error {
	if v.conn == nil {
		return errors.New("no connection to beacon RPC")
	}
	return nil
}

func (v *ValidatorService) recheckKeys(ctx context.Context) {
	var validatingKeys [][48]byte
	var err error
	if featureconfig.Get().EnableAccountsV2 {
		if v.useWeb {
			initializedChan := make(chan *wallet.Wallet)
			sub := v.walletInitializedFeed.Subscribe(initializedChan)
			defer sub.Unsubscribe()
			w := <-initializedChan
			keyManagerV2, err := w.InitializeKeymanager(
				ctx, true, /* skipMnemonicConfirm */
			)
			if err != nil {
				log.Fatalf("Could not read keymanager for wallet: %v", err)
			}
			v.keyManagerV2 = keyManagerV2
		}
		validatingKeys, err = v.keyManagerV2.FetchValidatingPublicKeys(ctx)
		if err != nil {
			log.WithError(err).Debug("Could not fetch validating keys")
		}
		if err := v.db.UpdatePublicKeysBuckets(validatingKeys); err != nil {
			log.WithError(err).Debug("Could not update public keys buckets")
		}
		go recheckValidatingKeysBucket(ctx, v.db, v.keyManagerV2)
	} else {
		validatingKeys, err = v.keyManager.FetchValidatingKeys()
		if err != nil {
			log.WithError(err).Debug("Could not fetch validating keys")
		}
		if err := v.db.UpdatePublicKeysBuckets(validatingKeys); err != nil {
			log.WithError(err).Debug("Could not update public keys buckets")
		}
	}
	for _, key := range validatingKeys {
		log.WithField(
			"publicKey", fmt.Sprintf("%#x", bytesutil.Trunc(key[:])),
		).Info("Validating for public key")
	}
}

// signObject signs a generic object, with protection if available.
// This should only be used for accounts v1.
func (v *validator) signObject(
	ctx context.Context,
	pubKey [48]byte,
	object interface{},
	domain []byte,
) (bls.Signature, error) {
	if featureconfig.Get().EnableAccountsV2 {
		return nil, errors.New("signObject not supported for accounts v2")
	}

	if protectingKeymanager, supported := v.keyManager.(keymanager.ProtectingKeyManager); supported {
		var root [32]byte
		var err error
		if v, ok := object.(fssz.HashRoot); ok {
			root, err = v.HashTreeRoot()
		} else {
			root, err = ssz.HashTreeRoot(object)
		}

		if err != nil {
			return nil, err
		}
		return protectingKeymanager.SignGeneric(pubKey, root, bytesutil.ToBytes32(domain))
	}

	root, err := helpers.ComputeSigningRoot(object, domain)
	if err != nil {
		return nil, err
	}
	return v.keyManager.Sign(ctx, pubKey, root)
}

// ConstructDialOptions constructs a list of grpc dial options
func ConstructDialOptions(
	maxCallRecvMsgSize int,
	withCert string,
	grpcHeaders []string,
	grpcRetries uint,
	grpcRetryDelay time.Duration,
	extraOpts ...grpc.DialOption,
) []grpc.DialOption {
	var transportSecurity grpc.DialOption
	if withCert != "" {
		creds, err := credentials.NewClientTLSFromFile(withCert, "")
		if err != nil {
			log.Errorf("Could not get valid credentials: %v", err)
			return nil
		}
		transportSecurity = grpc.WithTransportCredentials(creds)
	} else {
		transportSecurity = grpc.WithInsecure()
		log.Warn("You are using an insecure gRPC connection. If you are running your beacon node and " +
			"validator on the same machines, you can ignore this message. If you want to know " +
			"how to enable secure connections, see: https://docs.prylabs.network/docs/prysm-usage/secure-grpc")
	}

	if maxCallRecvMsgSize == 0 {
		maxCallRecvMsgSize = 10 * 5 << 20 // Default 50Mb
	}

	md := make(metadata.MD)
	for _, hdr := range grpcHeaders {
		if hdr != "" {
			ss := strings.Split(hdr, "=")
			if len(ss) != 2 {
				log.Warnf("Incorrect gRPC header flag format. Skipping %v", hdr)
				continue
			}
			md.Set(ss[0], ss[1])
		}
	}

	dialOpts := []grpc.DialOption{
		transportSecurity,
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxCallRecvMsgSize),
			grpc_retry.WithMax(grpcRetries),
			grpc_retry.WithBackoff(grpc_retry.BackoffLinear(grpcRetryDelay)),
			grpc.Header(&md),
		),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithUnaryInterceptor(middleware.ChainUnaryClient(
			grpc_opentracing.UnaryClientInterceptor(),
			grpc_prometheus.UnaryClientInterceptor,
			grpc_retry.UnaryClientInterceptor(),
			grpcutils.LogGRPCRequests,
		)),
		grpc.WithChainStreamInterceptor(
			grpcutils.LogGRPCStream,
			grpc_opentracing.StreamClientInterceptor(),
			grpc_prometheus.StreamClientInterceptor,
			grpc_retry.StreamClientInterceptor(),
		),
	}

	dialOpts = append(dialOpts, extraOpts...)
	return dialOpts
}

// Syncing returns whether or not the beacon node is currently synchronizing the chain.
func (v *ValidatorService) Syncing(ctx context.Context) (bool, error) {
	nc := ethpb.NewNodeClient(v.conn)
	resp, err := nc.GetSyncStatus(ctx, &ptypes.Empty{})
	if err != nil {
		return false, err
	}
	return resp.Syncing, nil
}

// GenesisInfo queries the beacon node for the chain genesis info containing
// the genesis time along with the validator deposit contract address.
func (v *ValidatorService) GenesisInfo(ctx context.Context) (*ethpb.Genesis, error) {
	nc := ethpb.NewNodeClient(v.conn)
	return nc.GetGenesis(ctx, &ptypes.Empty{})
}

// to accounts changes in the keymanager, then updates those keys'
// buckets in bolt DB if a bucket for a key does not exist.
func recheckValidatingKeysBucket(ctx context.Context, valDB db.Database, km v2.IKeymanager) {
	directKeymanager, ok := km.(*direct.Keymanager)
	if !ok {
		return
	}
	validatingPubKeysChan := make(chan [][48]byte, 1)
	sub := directKeymanager.SubscribeAccountChanges(validatingPubKeysChan)
	defer sub.Unsubscribe()
	for {
		select {
		case keys := <-validatingPubKeysChan:
			if err := valDB.UpdatePublicKeysBuckets(keys); err != nil {
				log.WithError(err).Debug("Could not update public keys buckets")
				continue
			}
		case <-ctx.Done():
			return
		}
	}
}
