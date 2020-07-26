package v2

import (
	"crypto/rand"
	"flag"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func init() {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(ioutil.Discard)
}

type testWalletConfig struct {
	walletDir        string
	passwordsDir     string
	exportDir        string
	accountsToExport string
	passwordFile     string
	keymanagerKind   v2keymanager.Kind
}

func setupWalletCtx(
	tb testing.TB,
	cfg *testWalletConfig,
) *cli.Context {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	set.String(flags.WalletDirFlag.Name, cfg.walletDir, "")
	set.String(flags.WalletPasswordsDirFlag.Name, cfg.passwordsDir, "")
	set.String(flags.KeymanagerKindFlag.Name, cfg.keymanagerKind.String(), "")
	set.String(flags.BackupDirFlag.Name, cfg.exportDir, "")
	set.String(flags.AccountsFlag.Name, cfg.accountsToExport, "")
	set.String(flags.PasswordFileFlag.Name, cfg.passwordFile, "")
	assert.NoError(tb, set.Set(flags.WalletDirFlag.Name, cfg.walletDir))
	assert.NoError(tb, set.Set(flags.WalletPasswordsDirFlag.Name, cfg.passwordsDir))
	assert.NoError(tb, set.Set(flags.KeymanagerKindFlag.Name, cfg.keymanagerKind.String()))
	assert.NoError(tb, set.Set(flags.BackupDirFlag.Name, cfg.exportDir))
	assert.NoError(tb, set.Set(flags.AccountsFlag.Name, cfg.accountsToExport))
	assert.NoError(tb, set.Set(flags.PasswordFileFlag.Name, cfg.passwordFile))
	return cli.NewContext(&app, set, nil)
}

func setupWalletAndPasswordsDir(t testing.TB) (string, string) {
	randPath, err := rand.Int(rand.Reader, big.NewInt(1000000))
	require.NoError(t, err, "Could not generate random file path")
	walletDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "wallet")
	require.NoError(t, os.RemoveAll(walletDir), "Failed to remove directory")
	passwordsDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "passwords")
	require.NoError(t, os.RemoveAll(passwordsDir), "Failed to remove directory")
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(walletDir), "Failed to remove directory")
		require.NoError(t, os.RemoveAll(passwordsDir), "Failed to remove directory")
	})
	return walletDir, passwordsDir
}

func TestCreateAndReadWallet(t *testing.T) {
	walletDir, passwordsDir := setupWalletAndPasswordsDir(t)
	cliCtx := setupWalletCtx(t, &testWalletConfig{
		walletDir:      walletDir,
		passwordsDir:   passwordsDir,
		keymanagerKind: v2keymanager.Direct,
	})
	wallet, err := NewWallet(cliCtx, v2keymanager.Direct)
	require.NoError(t, wallet.SaveWallet())
	require.NoError(t, err)
	// We should be able to now read the wallet as well.
	_, err = OpenWallet(cliCtx)
	require.NoError(t, err)
}
