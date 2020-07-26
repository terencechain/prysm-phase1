package v2

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/logrusorgru/aurora"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
	"github.com/urfave/cli/v2"
)

// ImportAccount uses the archived account made from ExportAccount to import an account and
// asks the users for account passwords.
func ImportAccount(cliCtx *cli.Context) error {
	walletDir, err := inputDirectory(cliCtx, walletDirPromptText, flags.WalletDirFlag)
	if err != nil && !errors.Is(err, ErrNoWalletFound) {
		return errors.Wrap(err, "could not parse wallet directory")
	}
	passwordsDir, err := inputDirectory(cliCtx, passwordsDirPromptText, flags.WalletPasswordsDirFlag)
	if err != nil {
		return err
	}
	backupDir, err := inputDirectory(cliCtx, importDirPromptText, flags.BackupDirFlag)
	if err != nil {
		return errors.Wrap(err, "could not parse output directory")
	}

	accountsPath := filepath.Join(walletDir, v2keymanager.Direct.String())
	if err := os.MkdirAll(accountsPath, DirectoryPermissions); err != nil {
		return errors.Wrap(err, "could not create wallet directory")
	}
	if err := os.MkdirAll(passwordsDir, DirectoryPermissions); err != nil {
		return errors.Wrap(err, "could not create passwords directory")
	}
	accountsImported, err := unzipArchiveToTarget(backupDir, filepath.Dir(walletDir))
	if err != nil {
		return errors.Wrap(err, "could not unzip archive")
	}

	wallet := &Wallet{
		accountsPath:   accountsPath,
		passwordsDir:   passwordsDir,
		keymanagerKind: v2keymanager.Direct,
	}

	au := aurora.NewAurora(true)
	var loggedAccounts []string
	for _, accountName := range accountsImported {
		loggedAccounts = append(loggedAccounts, fmt.Sprintf("%s", au.BrightGreen(accountName).Bold()))
	}
	fmt.Printf("Importing accounts: %s\n", strings.Join(loggedAccounts, ", "))

	for _, accountName := range accountsImported {
		if err := wallet.enterPasswordForAccount(cliCtx, accountName); err != nil {
			return errors.Wrap(err, "could not set account password")
		}
	}
	keymanager, err := wallet.InitializeKeymanager(context.Background(), true /* skip mnemonic confirm */)
	if err != nil {
		return errors.Wrap(err, "could not initialize keymanager")
	}
	km, ok := keymanager.(*direct.Keymanager)
	if !ok {
		return errors.New("can only export accounts for a non-HD wallet")
	}
	if err := logAccountsImported(wallet, km, accountsImported); err != nil {
		return errors.Wrap(err, "could not log accounts imported")
	}

	return nil
}

func unzipArchiveToTarget(archiveDir string, target string) ([]string, error) {
	archiveFile := filepath.Join(archiveDir, archiveFilename)
	reader, err := zip.OpenReader(archiveFile)
	if err != nil {
		return nil, errors.Wrap(err, "could not open reader for archive")
	}

	perms := os.FileMode(0700)
	if err := os.MkdirAll(target, perms); err != nil {
		return nil, errors.Wrap(err, "could not parent path for folder")
	}

	var accounts []string
	for _, file := range reader.File {
		path := filepath.Join(target, file.Name)
		parentFolder := filepath.Dir(path)
		if file.FileInfo().IsDir() {
			accounts = append(accounts, file.FileInfo().Name())
			if err := os.MkdirAll(path, perms); err != nil {
				return nil, errors.Wrap(err, "could not make path for file")
			}
			continue
		} else {
			if err := os.MkdirAll(parentFolder, perms); err != nil {
				return nil, errors.Wrap(err, "could not make path for file")
			}
		}

		if err := copyFileFromZipToPath(file, path); err != nil {
			return nil, err
		}
	}
	return accounts, nil
}

func copyFileFromZipToPath(file *zip.File, path string) error {
	fileReader, err := file.Open()
	if err != nil {
		return err
	}
	defer func() {
		if err := fileReader.Close(); err != nil {
			log.WithError(err).Error("Could not close file")
		}
	}()

	targetFile, err := os.Create(path)
	if err != nil {
		return errors.Wrap(err, "could not open file")
	}
	defer func() {
		if err := targetFile.Close(); err != nil {
			log.WithError(err).Error("Could not close target")
		}
	}()

	if _, err := io.Copy(targetFile, fileReader); err != nil {
		return errors.Wrap(err, "could not copy file")
	}
	return nil
}

func logAccountsImported(wallet *Wallet, keymanager *direct.Keymanager, accountNames []string) error {
	au := aurora.NewAurora(true)

	numAccounts := au.BrightYellow(len(accountNames))
	fmt.Println("")
	if len(accountNames) == 1 {
		fmt.Printf("Imported %d validator account\n", numAccounts)
	} else {
		fmt.Printf("Imported %d validator accounts\n", numAccounts)
	}
	for _, accountName := range accountNames {
		fmt.Println("")
		fmt.Printf("%s\n", au.BrightGreen(accountName).Bold())

		publicKey, err := keymanager.PublicKeyForAccount(accountName)
		if err != nil {
			return errors.Wrap(err, "could not get public key")
		}
		fmt.Printf("%s %#x\n", au.BrightMagenta("[public key]").Bold(), publicKey)

		dirPath := au.BrightCyan("(wallet dir)")
		fmt.Printf("%s %s\n", dirPath, filepath.Join(wallet.AccountsDir(), accountName))
	}
	return nil
}
