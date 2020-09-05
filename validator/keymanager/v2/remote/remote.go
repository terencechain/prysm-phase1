package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	ptypes "github.com/gogo/protobuf/types"
	"github.com/logrusorgru/aurora"
	"github.com/pkg/errors"
	validatorpb "github.com/prysmaticlabs/prysm/proto/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	log = logrus.WithField("prefix", "remote-keymanager-v2")
	// ErrSigningFailed defines a failure from the remote server
	// when performing a signing operation.
	ErrSigningFailed = errors.New("signing failed in the remote server")
	// ErrSigningDenied defines a failure from the remote server when
	// performing a signing operation was denied by a remote server.
	ErrSigningDenied = errors.New("signing request was denied by remote server")
)

// KeymanagerOpts for a remote keymanager.
type KeymanagerOpts struct {
	RemoteCertificate *CertificateConfig `json:"remote_cert"`
	RemoteAddr        string             `json:"remote_address"`
}

// CertificateConfig defines configuration options for
// certificate authority certs, client certs, and client keys
// for TLS gRPC connections.
type CertificateConfig struct {
	ClientCertPath string `json:"crt_path"`
	ClientKeyPath  string `json:"key_path"`
	CACertPath     string `json:"ca_crt_path"`
}

// SetupConfig includes configuration values for initializing
// a keymanager, such as passwords, the wallet, and more.
type SetupConfig struct {
	Opts           *KeymanagerOpts
	MaxMessageSize int
}

// Keymanager implementation using remote signing keys via gRPC.
type Keymanager struct {
	opts             *KeymanagerOpts
	client           validatorpb.RemoteSignerClient
	accountsByPubkey map[[48]byte]string
}

// NewKeymanager instantiates a new direct keymanager from configuration options.
func NewKeymanager(ctx context.Context, cfg *SetupConfig) (*Keymanager, error) {
	// Load the client certificates.
	if cfg.Opts.RemoteCertificate == nil {
		return nil, errors.New("certificates are required")
	}
	if cfg.Opts.RemoteCertificate.ClientCertPath == "" {
		return nil, errors.New("client certificate is required")
	}
	if cfg.Opts.RemoteCertificate.ClientKeyPath == "" {
		return nil, errors.New("client key is required")
	}
	clientPair, err := tls.LoadX509KeyPair(cfg.Opts.RemoteCertificate.ClientCertPath, cfg.Opts.RemoteCertificate.ClientKeyPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to obtain client's certificate and/or key")
	}

	// Load the CA for the server certificate if present.
	cp := x509.NewCertPool()
	if cfg.Opts.RemoteCertificate.CACertPath != "" {
		serverCA, err := ioutil.ReadFile(cfg.Opts.RemoteCertificate.CACertPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to obtain server's CA certificate")
		}
		if !cp.AppendCertsFromPEM(serverCA) {
			return nil, errors.Wrap(err, "failed to add server's CA certificate to pool")
		}
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{clientPair},
		RootCAs:      cp,
	}
	clientCreds := credentials.NewTLS(tlsCfg)

	grpcOpts := []grpc.DialOption{
		// Require TLS with client certificate.
		grpc.WithTransportCredentials(clientCreds),
		// Receive large messages without erroring.
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(cfg.MaxMessageSize)),
	}
	conn, err := grpc.Dial(cfg.Opts.RemoteAddr, grpcOpts...)
	if err != nil {
		return nil, errors.New("failed to connect to remote wallet")
	}
	client := validatorpb.NewRemoteSignerClient(conn)
	k := &Keymanager{
		opts:             cfg.Opts,
		client:           client,
		accountsByPubkey: make(map[[48]byte]string),
	}
	return k, nil
}

// UnmarshalOptionsFile attempts to JSON unmarshal a keymanager
// options file into a struct.
func UnmarshalOptionsFile(r io.ReadCloser) (*KeymanagerOpts, error) {
	enc, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Wrap(err, "could not read config")
	}
	defer func() {
		if err := r.Close(); err != nil {
			log.Errorf("Could not close keymanager config file: %v", err)
		}
	}()
	opts := &KeymanagerOpts{}
	if err := json.Unmarshal(enc, opts); err != nil {
		return nil, errors.Wrap(err, "could not JSON unmarshal")
	}
	return opts, nil
}

// MarshalOptionsFile for the keymanager.
func MarshalOptionsFile(ctx context.Context, cfg *KeymanagerOpts) ([]byte, error) {
	return json.MarshalIndent(cfg, "", "\t")
}

// String pretty-print of a remote keymanager options.
func (opts *KeymanagerOpts) String() string {
	au := aurora.NewAurora(true)
	var b strings.Builder
	strAddr := fmt.Sprintf("%s: %s\n", au.BrightMagenta("Remote gRPC address"), opts.RemoteAddr)
	if _, err := b.WriteString(strAddr); err != nil {
		log.Error(err)
		return ""
	}
	strCrt := fmt.Sprintf(
		"%s: %s\n", au.BrightMagenta("Client cert path"), opts.RemoteCertificate.ClientCertPath,
	)
	if _, err := b.WriteString(strCrt); err != nil {
		log.Error(err)
		return ""
	}
	strKey := fmt.Sprintf(
		"%s: %s\n", au.BrightMagenta("Client key path"), opts.RemoteCertificate.ClientKeyPath,
	)
	if _, err := b.WriteString(strKey); err != nil {
		log.Error(err)
		return ""
	}
	strCa := fmt.Sprintf(
		"%s: %s\n", au.BrightMagenta("CA cert path"), opts.RemoteCertificate.CACertPath,
	)
	if _, err := b.WriteString(strCa); err != nil {
		log.Error(err)
		return ""
	}
	return b.String()
}

// KeymanagerOpts for the remote keymanager.
func (k *Keymanager) KeymanagerOpts() *KeymanagerOpts {
	return k.opts
}

// FetchValidatingPublicKeys fetches the list of public keys that should be used to validate with.
func (k *Keymanager) FetchValidatingPublicKeys(ctx context.Context) ([][48]byte, error) {
	resp, err := k.client.ListValidatingPublicKeys(ctx, &ptypes.Empty{})
	if err != nil {
		return nil, errors.Wrap(err, "could not list accounts from remote server")
	}
	pubKeys := make([][48]byte, len(resp.ValidatingPublicKeys))
	for i := range resp.ValidatingPublicKeys {
		pubKeys[i] = bytesutil.ToBytes48(resp.ValidatingPublicKeys[i])
	}
	return pubKeys, nil
}

// Sign signs a message for a validator key via a gRPC request.
func (k *Keymanager) Sign(ctx context.Context, req *validatorpb.SignRequest) (bls.Signature, error) {
	resp, err := k.client.Sign(ctx, req)
	if err != nil {
		return nil, err
	}
	switch resp.Status {
	case validatorpb.SignResponse_DENIED:
		return nil, ErrSigningDenied
	case validatorpb.SignResponse_FAILED:
		return nil, ErrSigningFailed
	}
	return bls.SignatureFromBytes(resp.Signature)
}
