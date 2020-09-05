package v2

import (
	"flag"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/remote"
	"github.com/urfave/cli/v2"
)

func TestEditWalletConfiguration(t *testing.T) {
	walletDir, _, _ := setupWalletAndPasswordsDir(t)
	cliCtx := setupWalletCtx(t, &testWalletConfig{
		walletDir:      walletDir,
		keymanagerKind: v2keymanager.Remote,
	})
	wallet, err := CreateWalletWithKeymanager(cliCtx.Context, &CreateWalletConfig{
		WalletCfg: &WalletConfig{
			WalletDir:      walletDir,
			KeymanagerKind: v2keymanager.Remote,
			WalletPassword: "Passwordz0320$",
		},
	})
	require.NoError(t, err)

	originalCfg := &remote.KeymanagerOpts{
		RemoteCertificate: &remote.CertificateConfig{
			ClientCertPath: "/tmp/a.crt",
			ClientKeyPath:  "/tmp/b.key",
			CACertPath:     "/tmp/c.crt",
		},
		RemoteAddr: "my.server.com:4000",
	}
	encodedCfg, err := remote.MarshalOptionsFile(cliCtx.Context, originalCfg)
	assert.NoError(t, err)
	assert.NoError(t, wallet.WriteKeymanagerConfigToDisk(cliCtx.Context, encodedCfg))

	wantCfg := &remote.KeymanagerOpts{
		RemoteCertificate: &remote.CertificateConfig{
			ClientCertPath: "/tmp/client.crt",
			ClientKeyPath:  "/tmp/client.key",
			CACertPath:     "/tmp/ca.crt",
		},
		RemoteAddr: "host.example.com:4000",
	}
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	set.String(flags.WalletDirFlag.Name, walletDir, "")
	set.String(flags.GrpcRemoteAddressFlag.Name, wantCfg.RemoteAddr, "")
	set.String(flags.RemoteSignerCertPathFlag.Name, wantCfg.RemoteCertificate.ClientCertPath, "")
	set.String(flags.RemoteSignerKeyPathFlag.Name, wantCfg.RemoteCertificate.ClientKeyPath, "")
	set.String(flags.RemoteSignerCACertPathFlag.Name, wantCfg.RemoteCertificate.CACertPath, "")
	assert.NoError(t, set.Set(flags.WalletDirFlag.Name, walletDir))
	assert.NoError(t, set.Set(flags.GrpcRemoteAddressFlag.Name, wantCfg.RemoteAddr))
	assert.NoError(t, set.Set(flags.RemoteSignerCertPathFlag.Name, wantCfg.RemoteCertificate.ClientCertPath))
	assert.NoError(t, set.Set(flags.RemoteSignerKeyPathFlag.Name, wantCfg.RemoteCertificate.ClientKeyPath))
	assert.NoError(t, set.Set(flags.RemoteSignerCACertPathFlag.Name, wantCfg.RemoteCertificate.CACertPath))
	cliCtx = cli.NewContext(&app, set, nil)

	err = EditWalletConfigurationCli(cliCtx)
	require.NoError(t, err)
	encoded, err := wallet.ReadKeymanagerConfigFromDisk(cliCtx.Context)
	require.NoError(t, err)

	cfg, err := remote.UnmarshalOptionsFile(encoded)
	assert.NoError(t, err)
	assert.DeepEqual(t, wantCfg, cfg)
}
