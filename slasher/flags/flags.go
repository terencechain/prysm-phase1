// Package flags contains all configuration runtime flags for
// the slasher service.
package flags

import (
	"github.com/urfave/cli/v2"
)

var (
	// BeaconCertFlag defines a flag for the beacon api certificate.
	BeaconCertFlag = &cli.StringFlag{
		Name:  "beacon-tls-cert",
		Usage: "Certificate for secure beacon gRPC connection. Pass this in order to use beacon gRPC securely.",
	}
	// BeaconRPCProviderFlag defines a flag for the beacon host ip or address.
	BeaconRPCProviderFlag = &cli.StringFlag{
		Name:  "beacon-rpc-provider",
		Usage: "Beacon node RPC provider endpoint",
		Value: "localhost:4000",
	}
	// CertFlag defines a flag for the node's TLS certificate.
	CertFlag = &cli.StringFlag{
		Name:  "tls-cert",
		Usage: "Certificate for secure gRPC. Pass this and the tls-key flag in order to use gRPC securely.",
	}
	// KeyFlag defines a flag for the node's TLS key.
	KeyFlag = &cli.StringFlag{
		Name:  "tls-key",
		Usage: "Key for secure gRPC. Pass this and the tls-cert flag in order to use gRPC securely.",
	}
	// MonitoringPortFlag defines the http port used to serve prometheus metrics.
	MonitoringPortFlag = &cli.IntFlag{
		Name:  "monitoring-port",
		Usage: "Port used to listening and respond metrics for prometheus.",
		Value: 8082,
	}
	// RPCHost defines the host on which the RPC server should listen.
	RPCHost = &cli.StringFlag{
		Name:  "rpc-host",
		Usage: "Host on which the RPC server should listen",
		Value: "127.0.0.1",
	}
	// RPCPort defines a slasher node RPC port to open.
	RPCPort = &cli.IntFlag{
		Name:  "rpc-port",
		Usage: "RPC port exposed by the slasher",
		Value: 4002,
	}
	// EnableHistoricalDetectionFlag is a flag to enable historical detection for the slasher. Requires --historical-slasher-node on the beacon node.
	EnableHistoricalDetectionFlag = &cli.BoolFlag{
		Name:  "enable-historical-detection",
		Usage: "Enables historical attestation detection for the slasher. Requires --historical-slasher-node on the beacon node.",
	}
	// SpanCacheSize is a flag that sets the size of span cache.
	SpanCacheSize = &cli.IntFlag{
		Name:  "spans-cache-size",
		Usage: "Sets the span cache size.",
		Value: 1500,
	}
)
