package v2

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prysmaticlabs/prysm/shared/fileutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/wallet"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
)

func TestBackupAccounts_Noninteractive_Derived(t *testing.T) {
	walletDir, _, passwordFilePath := setupWalletAndPasswordsDir(t)
	//Specify the password locally to this file for convenience.
	password := "Pa$sW0rD0__Fo0xPr"
	require.NoError(t, ioutil.WriteFile(passwordFilePath, []byte(password), os.ModePerm))

	randPath, err := rand.Int(rand.Reader, big.NewInt(1000000))
	require.NoError(t, err, "Could not generate random file path")
	// Write a directory where we will backup accounts to.
	backupDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "backupDir")
	require.NoError(t, os.MkdirAll(backupDir, os.ModePerm))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(backupDir), "Failed to remove directory")
	})

	// Write a password for the accounts we wish to backup to a file.
	backupPasswordFile := filepath.Join(backupDir, "backuppass.txt")
	err = ioutil.WriteFile(
		backupPasswordFile,
		[]byte("Passw0rdz4938%%"),
		params.BeaconIoConfig().ReadWritePermissions,
	)
	require.NoError(t, err)

	// We initialize a wallet with a derived keymanager.
	cliCtx := setupWalletCtx(t, &testWalletConfig{
		// Wallet configuration flags.
		walletDir:          walletDir,
		keymanagerKind:     v2keymanager.Derived,
		walletPasswordFile: passwordFilePath,
		// Flags required for BackupAccounts to work.
		backupPasswordFile: backupPasswordFile,
		backupDir:          backupDir,
	})
	w, err := CreateWalletWithKeymanager(cliCtx.Context, &CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      walletDir,
			KeymanagerKind: v2keymanager.Derived,
			WalletPassword: password,
		},
	})
	require.NoError(t, err)

	// Create 2 accounts
	err = CreateAccount(cliCtx.Context, &CreateAccountConfig{
		Wallet:      w,
		NumAccounts: 2,
	})
	require.NoError(t, err)

	keymanager, err := w.InitializeKeymanager(
		cliCtx.Context,
		true, /* skip mnemonic confirm */
	)
	require.NoError(t, err)

	// Obtain the public keys of the accounts we created
	pubkeys, err := keymanager.FetchValidatingPublicKeys(cliCtx.Context)
	require.NoError(t, err)
	var generatedPubKeys []string
	for _, pubkey := range pubkeys {
		encoded := make([]byte, hex.EncodedLen(len(pubkey)))
		hex.Encode(encoded, pubkey[:])
		generatedPubKeys = append(generatedPubKeys, string(encoded))
	}
	backupPublicKeys := strings.Join(generatedPubKeys, ",")

	// Recreate a cliCtx with the addition of these backup keys to be later used by the backup process
	cliCtx = setupWalletCtx(t, &testWalletConfig{
		// Wallet configuration flags.
		walletDir:          walletDir,
		keymanagerKind:     v2keymanager.Derived,
		walletPasswordFile: passwordFilePath,
		// Flags required for BackupAccounts to work.
		backupPublicKeys:   backupPublicKeys,
		backupPasswordFile: backupPasswordFile,
		backupDir:          backupDir,
	})

	// Next, we attempt to backup the accounts.
	require.NoError(t, BackupAccountsCli(cliCtx))

	// We check a backup.zip file was created at the output path.
	zipFilePath := filepath.Join(backupDir, archiveFilename)
	assert.DeepEqual(t, true, fileutil.FileExists(zipFilePath))

	// We attempt to unzip the file and verify the keystores do match our accounts.
	f, err := os.Open(zipFilePath)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()
	fi, err := f.Stat()
	require.NoError(t, err)
	r, err := zip.NewReader(f, fi.Size())
	require.NoError(t, err)

	// We check we have 2 keystore files in the unzipped results.
	require.DeepEqual(t, 2, len(r.File))
	unzippedPublicKeys := make([]string, 2)
	for i, unzipped := range r.File {
		ff, err := unzipped.Open()
		require.NoError(t, err)
		encodedBytes, err := ioutil.ReadAll(ff)
		require.NoError(t, err)
		keystoreFile := &v2keymanager.Keystore{}
		require.NoError(t, json.Unmarshal(encodedBytes, keystoreFile))
		require.NoError(t, ff.Close())
		unzippedPublicKeys[i] = keystoreFile.Pubkey
	}
	sort.Strings(unzippedPublicKeys)
	sort.Strings(generatedPubKeys)
	assert.DeepEqual(t, unzippedPublicKeys, generatedPubKeys)
}

func TestBackupAccounts_Noninteractive_Direct(t *testing.T) {
	walletDir, _, passwordFilePath := setupWalletAndPasswordsDir(t)
	randPath, err := rand.Int(rand.Reader, big.NewInt(1000000))
	require.NoError(t, err, "Could not generate random file path")
	// Write a directory where we will import keys from.
	keysDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "keysDir")
	require.NoError(t, os.MkdirAll(keysDir, os.ModePerm))

	// Write a directory where we will backup accounts to.
	backupDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "backupDir")
	require.NoError(t, os.MkdirAll(backupDir, os.ModePerm))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(keysDir), "Failed to remove directory")
		require.NoError(t, os.RemoveAll(backupDir), "Failed to remove directory")
	})

	// Create 2 keystore files in the keys directory we can then
	// import from in our wallet.
	k1, _ := createKeystore(t, keysDir)
	time.Sleep(time.Second)
	k2, _ := createKeystore(t, keysDir)
	generatedPubKeys := []string{k1.Pubkey, k2.Pubkey}
	backupPublicKeys := strings.Join(generatedPubKeys, ",")

	// Write a password for the accounts we wish to backup to a file.
	backupPasswordFile := filepath.Join(backupDir, "backuppass.txt")
	err = ioutil.WriteFile(
		backupPasswordFile,
		[]byte("Passw0rdz4938%%"),
		params.BeaconIoConfig().ReadWritePermissions,
	)
	require.NoError(t, err)

	// We initialize a wallet with a direct keymanager.
	cliCtx := setupWalletCtx(t, &testWalletConfig{
		// Wallet configuration flags.
		walletDir:           walletDir,
		keymanagerKind:      v2keymanager.Direct,
		walletPasswordFile:  passwordFilePath,
		accountPasswordFile: passwordFilePath,
		// Flags required for ImportAccounts to work.
		keysDir: keysDir,
		// Flags required for BackupAccounts to work.
		backupPublicKeys:   backupPublicKeys,
		backupPasswordFile: backupPasswordFile,
		backupDir:          backupDir,
	})
	_, err = CreateWalletWithKeymanager(cliCtx.Context, &CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      walletDir,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: "Passwordz0320$",
		},
	})
	require.NoError(t, err)

	// We attempt to import accounts we wrote to the keys directory
	// into our newly created wallet.
	require.NoError(t, ImportAccountsCli(cliCtx))

	// Next, we attempt to backup the accounts.
	require.NoError(t, BackupAccountsCli(cliCtx))

	// We check a backup.zip file was created at the output path.
	zipFilePath := filepath.Join(backupDir, archiveFilename)
	assert.DeepEqual(t, true, fileutil.FileExists(zipFilePath))

	// We attempt to unzip the file and verify the keystores do match our accounts.
	f, err := os.Open(zipFilePath)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()
	fi, err := f.Stat()
	require.NoError(t, err)
	r, err := zip.NewReader(f, fi.Size())
	require.NoError(t, err)

	// We check we have 2 keystore files in the unzipped results.
	require.DeepEqual(t, 2, len(r.File))
	unzippedPublicKeys := make([]string, 2)
	for i, unzipped := range r.File {
		ff, err := unzipped.Open()
		require.NoError(t, err)
		encodedBytes, err := ioutil.ReadAll(ff)
		require.NoError(t, err)
		keystoreFile := &v2keymanager.Keystore{}
		require.NoError(t, json.Unmarshal(encodedBytes, keystoreFile))
		require.NoError(t, ff.Close())
		unzippedPublicKeys[i] = keystoreFile.Pubkey
	}
	sort.Strings(unzippedPublicKeys)
	sort.Strings(generatedPubKeys)
	assert.DeepEqual(t, unzippedPublicKeys, generatedPubKeys)
}
