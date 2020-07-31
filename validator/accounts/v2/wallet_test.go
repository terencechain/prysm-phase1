package v2

import (
	"crypto/rand"
	"flag"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

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
	walletDir           string
	passwordsDir        string
	exportDir           string
	keysDir             string
	accountsToExport    string
	walletPasswordFile  string
	accountPasswordFile string
	numAccounts         int64
	keymanagerKind      v2keymanager.Kind
}

func setupWalletCtx(
	tb testing.TB,
	cfg *testWalletConfig,
) *cli.Context {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	set.String(flags.WalletDirFlag.Name, cfg.walletDir, "")
	set.String(flags.WalletPasswordsDirFlag.Name, cfg.passwordsDir, "")
	set.String(flags.KeysDirFlag.Name, cfg.keysDir, "")
	set.String(flags.KeymanagerKindFlag.Name, cfg.keymanagerKind.String(), "")
	set.String(flags.BackupDirFlag.Name, cfg.exportDir, "")
	set.String(flags.AccountsFlag.Name, cfg.accountsToExport, "")
	set.String(flags.WalletPasswordFileFlag.Name, cfg.walletPasswordFile, "")
	set.String(flags.AccountPasswordFileFlag.Name, cfg.accountPasswordFile, "")
	set.Bool(flags.SkipMnemonicConfirmFlag.Name, true, "")
	set.Int64(flags.NumAccountsFlag.Name, cfg.numAccounts, "")
	assert.NoError(tb, set.Set(flags.WalletDirFlag.Name, cfg.walletDir))
	assert.NoError(tb, set.Set(flags.WalletPasswordsDirFlag.Name, cfg.passwordsDir))
	assert.NoError(tb, set.Set(flags.KeysDirFlag.Name, cfg.keysDir))
	assert.NoError(tb, set.Set(flags.KeymanagerKindFlag.Name, cfg.keymanagerKind.String()))
	assert.NoError(tb, set.Set(flags.BackupDirFlag.Name, cfg.exportDir))
	assert.NoError(tb, set.Set(flags.AccountsFlag.Name, cfg.accountsToExport))
	assert.NoError(tb, set.Set(flags.WalletPasswordFileFlag.Name, cfg.walletPasswordFile))
	assert.NoError(tb, set.Set(flags.AccountPasswordFileFlag.Name, cfg.accountPasswordFile))
	assert.NoError(tb, set.Set(flags.SkipMnemonicConfirmFlag.Name, "true"))
	assert.NoError(tb, set.Set(flags.NumAccountsFlag.Name, strconv.Itoa(int(cfg.numAccounts))))
	return cli.NewContext(&app, set, nil)
}

func setupWalletAndPasswordsDir(t testing.TB) (string, string, string) {
	randPath, err := rand.Int(rand.Reader, big.NewInt(1000000))
	require.NoError(t, err, "Could not generate random file path")
	walletDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "wallet")
	require.NoError(t, os.RemoveAll(walletDir), "Failed to remove directory")
	passwordsDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "passwords")
	require.NoError(t, os.RemoveAll(passwordsDir), "Failed to remove directory")
	passwordFileDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "passwordFile")
	require.NoError(t, os.MkdirAll(passwordFileDir, os.ModePerm))
	passwordFilePath := filepath.Join(passwordFileDir, passwordFileName)
	require.NoError(t, ioutil.WriteFile(passwordFilePath, []byte(password), os.ModePerm))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(walletDir), "Failed to remove directory")
		require.NoError(t, os.RemoveAll(passwordFileDir), "Failed to remove directory")
		require.NoError(t, os.RemoveAll(passwordsDir), "Failed to remove directory")
	})
	return walletDir, passwordsDir, passwordFilePath
}

func TestCreateAndReadWallet(t *testing.T) {
	walletDir, passwordsDir, _ := setupWalletAndPasswordsDir(t)
	cliCtx := setupWalletCtx(t, &testWalletConfig{
		walletDir:      walletDir,
		passwordsDir:   passwordsDir,
		keymanagerKind: v2keymanager.Direct,
	})
	wallet, err := NewWallet(cliCtx, v2keymanager.Direct)
	require.NoError(t, err)
	require.NoError(t, createDirectKeymanagerWallet(cliCtx, wallet))
	// We should be able to now read the wallet as well.
	_, err = OpenWallet(cliCtx)
	require.NoError(t, err)
}

func TestAccountTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     time.Time
		wantErr  bool
	}{
		{
			name:     "keystore with timestamp",
			fileName: "keystore-1234567.json",
			want:     time.Unix(1234567, 0),
		},
		{
			name:     "keystore with deriv path and timestamp",
			fileName: "keystore-12313-313-00-0-5500550.json",
			want:     time.Unix(5500550, 0),
		},
		{
			name:     "keystore with no timestamp",
			fileName: "keystore.json",
			want:     time.Unix(0, 0),
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AccountTimestamp(tt.fileName)
			if (err != nil) != tt.wantErr {
				t.Errorf("AccountTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AccountTimestamp() got = %v, want %v", got, tt.want)
			}
		})
	}
}
