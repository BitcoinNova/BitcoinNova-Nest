// Copyright (c) 2018, The Bitcoin Nova Developers
//
// Please see the included LICENSE file for more information.
//

// Package walletdmanager handles the management of the wallet and the communication with the core wallet software
package walletdmanager

import (
	"BitcoinNova-Nest/bitcoinnovawalletdrpcgo"
	"bufio"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mitchellh/go-ps"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var (
	// WalletAvailableBalance is the available balance
	WalletAvailableBalance float64

	// WalletAddress is the wallet address
	WalletAddress string

	// WalletFilename is the filename of the opened wallet
	WalletFilename = ""

	// will be set to a random string when starting Bitcoinnova-service
	rpcPassword = ""

	cmdWalletd     *exec.Cmd
	cmdBitcoinnovad *exec.Cmd

	// WalletdOpenAndRunning is true when Bitcoinnova-service is running with a wallet open
	WalletdOpenAndRunning = false

	// WalletdSynced is true when wallet is synced and transfer is allowed
	WalletdSynced = false

	isPlatformDarwin  = false
	isPlatformLinux   = true
	isPlatformWindows = false

	// fee to be paid to node per transaction
	NodeFee float64
)

// Setup sets up some settings. It must be called at least once at the beginning of your program.
// platform should be set based on your platform. The choices are "linux", "darwin", "windows"
func Setup(platform string) {

	isPlatformDarwin = false
	isPlatformLinux = false
	isPlatformWindows = false

	switch platform {
	case "darwin":
		isPlatformDarwin = true
	case "windows":
		isPlatformWindows = true
	case "linux":
		isPlatformLinux = true
	default:
		isPlatformLinux = true
	}
}

// RequestBalance provides the available and locked balances of the current wallet
func RequestBalance() (availableBalance float64, lockedBalance float64, totalBalance float64, err error) {

	availableBalance, lockedBalance, totalBalance, err = bitcoinnovawalletdrpcgo.RequestBalance(rpcPassword)
	if err != nil {
		log.Error("error requesting balances. err: ", err)
	} else {
		WalletAvailableBalance = availableBalance
	}
	return availableBalance, lockedBalance, totalBalance, err
}

// RequestAvailableBalanceToBeSpent returns the available balance minus the fee
func RequestAvailableBalanceToBeSpent(transferFeeString string) (availableBalance float64, err error) {

	availableBalance, _, _, err = RequestBalance()
	if err != nil {
		return 0, err
	}

	transferFee, err := strconv.ParseFloat(transferFeeString, 64) // transferFee is expressed in BTN
	if err != nil {
		return 0, errors.New("fee is invalid")
	}

	if transferFee < 0 {
		return 0, errors.New("fee should be positive")
	}

	availableBalance = availableBalance - transferFee - NodeFee
	if availableBalance < 0 {
		availableBalance = 0
	}

	return availableBalance, nil
}

// RequestAddress provides the address of the current wallet
func RequestAddress() (address string, err error) {

	address, err = bitcoinnovawalletdrpcgo.RequestAddress(rpcPassword)
	if err != nil {
		log.Error("error requesting address. err: ", err)
	} else {
		WalletAddress = address
	}
	return address, err
}

// RequestListTransactions provides the list of transactions of current wallet
func RequestListTransactions() (transfers []bitcoinnovawalletdrpcgo.Transfer, err error) {

	walletBlockCount, _, _, _, err := bitcoinnovawalletdrpcgo.RequestStatus(rpcPassword)
	if err != nil {
		log.Error("error getting block count: ", err)
		return nil, err
	}

	transfers, err = bitcoinnovawalletdrpcgo.RequestListTransactions(walletBlockCount, 1, []string{WalletAddress}, rpcPassword)
	if err != nil {
		log.Error("error requesting list transactions. err: ", err)
	}
	return transfers, err
}

// SendTransaction makes a transfer with the provided information
func SendTransaction(transferAddress string, transferAmountString string, transferPaymentID string, transferFeeString string) (transactionHash string, err error) {

	if !WalletdSynced {
		return "", errors.New("wallet and/or blockchain not fully synced yet")
	}

	if !strings.HasPrefix(transferAddress, "E") || (len(transferAddress) != 95 && len(transferAddress) != 183) {
		return "", errors.New("address is invalid")
	}

	if transferAddress == WalletAddress {
		return "", errors.New("sending to yourself is not supported")
	}

	transferAmount, err := strconv.ParseFloat(transferAmountString, 64) // transferAmount is expressed in BTN
	if err != nil {
		return "", errors.New("amount is invalid")
	}

	if transferAmount <= 0 {
		return "", errors.New("amount of BTN to be sent should be greater than 0")
	}

	transferFee, err := strconv.ParseFloat(transferFeeString, 64) // transferFee is expressed in BTN
	if err != nil {
		return "", errors.New("fee is invalid")
	}

	if transferFee < 0 {
		return "", errors.New("fee should be positive")
	}

	if transferAmount+transferFee+NodeFee > WalletAvailableBalance {
		return "", errors.New("your available balance is insufficient")
	}

	transactionHash, err = bitcoinnovawalletdrpcgo.SendTransaction(transferAddress, transferAmount, transferPaymentID, transferFee, rpcPassword)
	if err != nil {
		log.Error("error sending transaction. err: ", err)
		return "", err
	}
	return transactionHash, nil
}

// OptimizeWalletWithFusion sends a fusion transaction to optimize the wallet
func OptimizeWalletWithFusion() (transactionHash string, err error) {

	_, smallestOptimizedThreshold, err := getOptimisedFusionParameters()
	if err != nil {
		return "", errors.Wrap(err, "getOptimisedFusionParameters failed")
	}

	transactionHash, err = bitcoinnovawalletdrpcgo.SendFusionTransaction(smallestOptimizedThreshold, []string{WalletAddress}, WalletAddress, rpcPassword)
	if err != nil {
		log.Error("error sending fusion transaction. err: ", err)
		return "", errors.Wrap(err, "sending fusion transaction failed")
	}

	return transactionHash, nil
}

func getOptimisedFusionParameters() (largestFusionReadyCount int, smallestOptimizedThreshold int, err error) {

	threshold := int(WalletAvailableBalance) + 1

	largestFusionReadyCount = -1
	smallestOptimizedThreshold = threshold

	for {
		fusionReadyCount, _, err := bitcoinnovawalletdrpcgo.EstimateFusion(threshold, []string{WalletAddress}, rpcPassword)
		if err != nil {
			log.Error("error estimating fusion. err: ", err)
			return 0, 0, err
		}

		if fusionReadyCount < largestFusionReadyCount {
			break
		}

		largestFusionReadyCount = fusionReadyCount
		smallestOptimizedThreshold = threshold
		threshold /= 2
	}

	return
}

// GetPrivateKeys provides the private view and spend keys of the current wallet, and the mnemonic seed if the wallet is deterministic
func GetPrivateKeys() (isDeterministicWallet bool, mnemonicSeed string, privateViewKey string, privateSpendKey string, err error) {

	isDeterministicWallet, mnemonicSeed, err = bitcoinnovawalletdrpcgo.GetMnemonicSeed(WalletAddress, rpcPassword)
	if err != nil {
		log.Error("error requesting mnemonic seed. err: ", err)
		return false, "", "", "", err
	}

	privateViewKey, err = bitcoinnovawalletdrpcgo.GetViewKey(rpcPassword)
	if err != nil {
		log.Error("error requesting view key. err: ", err)
		return false, "", "", "", err
	}

	privateSpendKey, _, err = bitcoinnovawalletdrpcgo.GetSpendKeys(WalletAddress, rpcPassword)
	if err != nil {
		log.Error("error requesting spend keys. err: ", err)
		return false, "", "", "", err
	}

	return isDeterministicWallet, mnemonicSeed, privateViewKey, privateSpendKey, nil
}

// SaveWallet saves the sync status of the wallet. To be done regularly so when Bitcoinnova-service crashes, sync is not lost
func SaveWallet() (err error) {

	err = bitcoinnovawalletdrpcgo.SaveWallet(rpcPassword)
	if err != nil {
		log.Error("error saving wallet. err: ", err)
		return err
	}

	return nil
}

// StartWalletd starts the Bitcoinnova-service daemon with the set wallet info
// walletPath is the full path to the wallet
// walletPassword is the wallet password
// useRemoteNode is true if remote node, false if local
// useCheckpoints is true if Bitcoinnovad should be run with "--load-checkpoints"
func StartWalletd(walletPath string, walletPassword string, useRemoteNode bool, useCheckpoints bool, daemonAddress string, daemonPort string) (err error) {

	if isWalletdRunning() {
		errorMessage := "Bitcoinnova-service is already running in the background.\nPlease close it via "

		if isPlatformWindows {
			errorMessage += "the task manager"
		} else if isPlatformDarwin {
			errorMessage += "the activity monitor"
		} else if isPlatformLinux {
			errorMessage += "a system monitor app"
		}
		errorMessage += "."

		return errors.New(errorMessage)
	}

	pathToLogWalletdCurrentSession := logWalletdCurrentSessionFilename
	pathToLogWalletdAllSessions := logWalletdAllSessionsFilename
	pathToLogBitcoinnovadCurrentSession := logBitcoinnovadCurrentSessionFilename
	pathToLogBitcoinnovadAllSessions := logBitcoinnovadAllSessionsFilename
	pathToWalletd := "./" + walletdCommandName
	pathToBitcoinnovad := "./" + bitcoinnovadCommandName
	checkpointsCSVFile := "checkpoints.csv"
	pathToCheckpointsCSV := "./" + checkpointsCSVFile

	WalletFilename = filepath.Base(walletPath)
	pathToWallet := filepath.Clean(walletPath)

	pathToAppDirectory, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal("error finding current directory. Error: ", err)
	}

	if isPlatformWindows {
		pathToWallet = strings.Replace(pathToWallet, "file:\\", "", 1)
	} else {
		pathToWallet = strings.Replace(pathToWallet, "file:", "", 1)
	}

	if isPlatformDarwin {
		pathToAppContents := filepath.Dir(pathToAppDirectory)
		pathToWalletd = pathToAppContents + "/" + walletdCommandName
		pathToBitcoinnovad = pathToAppContents + "/" + bitcoinnovadCommandName
		pathToCheckpointsCSV = pathToAppContents + "/" + checkpointsCSVFile

		usr, err := user.Current()
		if err != nil {
			log.Fatal("error finding home directory. Error: ", err)
		}
		pathToHomeDir := usr.HomeDir
		pathToAppLibDir := pathToHomeDir + "/Library/Application Support/BitcoinNova-Nest"

		pathToLogWalletdCurrentSession = pathToAppLibDir + "/" + logWalletdCurrentSessionFilename
		pathToLogWalletdAllSessions = pathToAppLibDir + "/" + logWalletdAllSessionsFilename
		pathToLogBitcoinnovadCurrentSession = pathToAppLibDir + "/" + logBitcoinnovadCurrentSessionFilename
		pathToLogBitcoinnovadAllSessions = pathToAppLibDir + "/" + logBitcoinnovadAllSessionsFilename

		if pathToWallet == WalletFilename {
			// if comes from createWallet, so it is not a full path, just a filename
			pathToWallet = pathToHomeDir + "/" + pathToWallet
		}
	} else if isPlatformLinux {
		pathToWalletd = pathToAppDirectory + "/" + walletdCommandName
		pathToBitcoinnovad = pathToAppDirectory + "/" + bitcoinnovadCommandName
		pathToCheckpointsCSV = pathToAppDirectory + "/" + checkpointsCSVFile
		pathToLogWalletdCurrentSession = pathToAppDirectory + "/" + logWalletdCurrentSessionFilename
		pathToLogWalletdAllSessions = pathToAppDirectory + "/" + logWalletdAllSessionsFilename
		pathToLogBitcoinnovadCurrentSession = pathToAppDirectory + "/" + logBitcoinnovadCurrentSessionFilename
		pathToLogBitcoinnovadAllSessions = pathToAppDirectory + "/" + logBitcoinnovadAllSessionsFilename
		if pathToWallet == WalletFilename {
			// if comes from createWallet, so it is not a full path, just a filename
			pathToWallet = pathToAppDirectory + "/" + pathToWallet
		}
	}

	// setup current session log file (logs are added real time in this file)
	walletdCurrentSessionLogFile, err := os.Create(pathToLogWalletdCurrentSession)
	if err != nil {
		log.Error(err)
	}
	defer walletdCurrentSessionLogFile.Close()

	rpcPassword = randStringBytesMaskImprSrc(20)

	var bitcoinNovadCurrentSessionLogFile *os.File

	if useRemoteNode {
		cmdWalletd = exec.Command(pathToWalletd, "-w", pathToWallet, "-p", walletPassword, "-l", pathToLogWalletdCurrentSession, "--daemon-address", daemonAddress, "--daemon-port", daemonPort, "--log-level", walletdLogLevel, "--rpc-password", rpcPassword)
	} else {
		cmdWalletd = exec.Command(pathToWalletd, "-w", pathToWallet, "-p", walletPassword, "-l", pathToLogWalletdCurrentSession, "--log-level", walletdLogLevel, "--rpc-password", rpcPassword)
	}
	hideCmdWindowIfNeeded(cmdWalletd)

	if !useRemoteNode && !isBitcoinnovadRunning() {

		bitcoinNovadCurrentSessionLogFile, err = os.Create(pathToLogBitcoinnovadCurrentSession)
		if err != nil {
			log.Error(err)
		}
		defer bitcoinNovadCurrentSessionLogFile.Close()

		if useCheckpoints {
			cmdBitcoinnovad = exec.Command(pathToBitcoinnovad, "--load-checkpoints", pathToCheckpointsCSV, "--log-file", pathToLogBitcoinnovadCurrentSession)
		} else {
			cmdBitcoinnovad = exec.Command(pathToBitcoinnovad, "--log-file", pathToLogBitcoinnovadCurrentSession)
		}
		hideCmdWindowIfNeeded(cmdBitcoinnovad)

		bitcoinNovadAllSessionsLogFile, err := os.Create(pathToLogBitcoinnovadAllSessions)
		if err != nil {
			log.Error(err)
		}
		cmdBitcoinnovad.Stdout = bitcoinNovadAllSessionsLogFile
		defer bitcoinNovadAllSessionsLogFile.Close()

		err = cmdBitcoinnovad.Start()
		if err != nil {
			log.Error(err)
			return err
		}

		log.Info("Opening Bitcoinnovad and waiting for it to be ready.")

		readerBitcoinnovadLog := bufio.NewReader(bitcoinNovadCurrentSessionLogFile)

		for {
			line, err := readerBitcoinnovadLog.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Error("Failed reading Bitcoinnovad log file line by line: ", err)
				}
			}
			if strings.Contains(line, "Imported block with index") {
				log.Info("Bitcoinnovad importing blocks: ", line)
			}
			if strings.Contains(line, "Core rpc server started ok") {
				log.Info("Bitcoinnovad ready (rpc server started ok).")
				break
			}
			if strings.Contains(line, "Node stopped.") {
				errorMessage := "Error Bitcoinnovad: 'Node stopped'"
				log.Error(errorMessage)
				return errors.New(errorMessage)
			}
		}
	}

	// setup all sessions log file
	walletdAllSessionsLogFile, err := os.OpenFile(pathToLogWalletdAllSessions, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Fatal(err)
	}
	cmdWalletd.Stdout = walletdAllSessionsLogFile
	defer walletdAllSessionsLogFile.Close()

	err = cmdWalletd.Start()
	if err != nil {
		log.Error(err)
		return err
	}

	log.Info("Opening Bitcoinnova-service and waiting for it to be ready.")

	timesCheckLog := 0
	timeBetweenChecks := 100 * time.Millisecond
	maxWaitingTime := 15 * time.Second
	successLaunchingWalletd := false
	var listWalletdErrors []string

	readerWalletdLog := bufio.NewReader(walletdCurrentSessionLogFile)

	for !successLaunchingWalletd && time.Duration(timesCheckLog)*timeBetweenChecks < maxWaitingTime {
		timesCheckLog++
		time.Sleep(timeBetweenChecks)
		for {
			line, err := readerWalletdLog.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Error("Failed reading log file line by line: ", err)
				}
				break
			}
			if strings.Contains(line, "Wallet loading is finished.") {
				successLaunchingWalletd = true
				log.Info("Bitcoinnova-service ready ('Wallet loading is finished.').")
				break
			}
			if strings.Contains(line, "Imported block with index") {
				timesCheckLog = 0
			}
			if strings.Contains(line, "INFO    Stopped") {
				errorMessage := ""

				if len(listWalletdErrors) > 0 {
					for _, line := range listWalletdErrors {
						errorMessage = errorMessage + line
					}
				} else {
					errorMessage = "Bitcoinnova-service stopped with unknown error"
				}

				killWalletd()
				return errors.New(errorMessage)
			}
			identifierErrorMessage := " ERROR  "
			if strings.Contains(line, identifierErrorMessage) {
				splitLine := strings.Split(line, identifierErrorMessage)
				listWalletdErrors = append(listWalletdErrors, splitLine[len(splitLine)-1])
			}
		}
	}

	// check rpc connection with walletd
	_, _, _, _, err = bitcoinnovawalletdrpcgo.RequestStatus(rpcPassword)
	if err != nil {
		killWalletd()
		return errors.New("error communicating with Bitcoinnova-service via rpc")
	}

	WalletdOpenAndRunning = true

	// time.Sleep(5 * time.Second)

	return nil
}

// GracefullyQuitWalletd stops the walletd daemon
func GracefullyQuitWalletd() {

	if WalletdOpenAndRunning || cmdWalletd != nil {
		var err error

		if isPlatformWindows {
			// because syscall.SIGTERM does not work in windows. We have to manually save the wallet, as we kill walletd.
			bitcoinnovawalletdrpcgo.SaveWallet(rpcPassword)
			time.Sleep(3 * time.Second)

			err = cmdWalletd.Process.Kill()
			if err != nil {
				log.Error("failed to kill Bitcoinnova-service: " + err.Error())
			} else {
				log.Info("Bitcoinnova-service killed without error")
			}
		} else {
			_ = cmdWalletd.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() {
				done <- cmdWalletd.Wait()
			}()
			select {
			case <-time.After(5 * time.Second):
				if err := cmdWalletd.Process.Kill(); err != nil {
					log.Warning("failed to kill Bitcoinnova-service: " + err.Error())
				}
				log.Info("Bitcoinnova-service killed as stopping process timed out")
			case err := <-done:
				if err != nil {
					log.Warning("Bitcoinnova-service finished with error: " + err.Error())
				}
				log.Info("Bitcoinnova-service killed without error")
			}
		}
	}

	WalletAvailableBalance = 0
	WalletAddress = ""
	WalletFilename = ""
	cmdWalletd = nil
	WalletdOpenAndRunning = false
}

// to make sure that after creating a wallet, there is no walletd process remaining at all
func killWalletd() {

	if cmdWalletd != nil {
		if isPlatformWindows {
			cmdWalletd.Process.Kill()
		} else {
			done := make(chan error, 1)
			go func() {
				done <- cmdWalletd.Wait()
			}()
			select {
			case <-time.After(500 * time.Millisecond):
				if err := cmdWalletd.Process.Kill(); err != nil {
					log.Warning("failed to kill Bitcoinnova-service: " + err.Error())
				}
				log.Info("Bitcoinnova-service killed as stopping process timed out")
			case err := <-done:
				if err != nil {
					log.Warning("Bitcoinnova-service finished with error: " + err.Error())
				}
				log.Info("Bitcoinnova-service killed without error")
			}
		}
	}
}

// GracefullyQuitBitcoinnovad stops the Bitcoinnovad daemon
func GracefullyQuitBitcoinnovad() {

	if cmdBitcoinnovad != nil {
		var err error

		if isPlatformWindows {
			// because syscall.SIGTERM does not work in windows. We have to kill Bitcoinnovad.

			err = cmdBitcoinnovad.Process.Kill()
			if err != nil {
				log.Error("failed to kill Bitcoinnovad: " + err.Error())
			} else {
				log.Info("Bitcoinnovad killed without error")
			}
		} else {
			_ = cmdBitcoinnovad.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() {
				done <- cmdBitcoinnovad.Wait()
			}()
			select {
			case <-time.After(5 * time.Second):
				if err := cmdBitcoinnovad.Process.Kill(); err != nil {
					log.Warning("failed to kill Bitcoinnovad: " + err.Error())
				}
				log.Info("Bitcoinnovad killed as stopping process timed out")
			case err := <-done:
				if err != nil {
					log.Warning("Bitcoinnovad finished with error: " + err.Error())
				}
				log.Info("Bitcoinnovad killed without error")
			}
		}
	}

	cmdBitcoinnovad = nil
}

// CreateWallet calls Bitcoinnova-service to create a new wallet. If privateViewKey, privateSpendKey and mnemonicSeed are empty strings, a new wallet will be generated. If they are not empty, a wallet will be generated from those keys or from the seed (import)
// walletFilename is the filename chosen by the user. The created wallet file will be located in the same folder as Bitcoinnova-service.
// walletPassword is the password of the new wallet.
// walletPasswordConfirmation is the repeat of the password for confirmation that the password was correctly entered.
// privateViewKey is the private view key of the wallet.
// privateSpendKey is the private spend key of the wallet.
// mnemonicSeed is the mnemonic seed for generating the wallet
func CreateWallet(walletFilename string, walletPassword string, walletPasswordConfirmation string, privateViewKey string, privateSpendKey string, mnemonicSeed string, scanHeight string) (err error) {

	if WalletdOpenAndRunning {
		return errors.New("Bitcoinnova-service is already running. It should be stopped before being able to generate a new wallet")
	}

	if strings.Contains(walletFilename, "/") || strings.Contains(walletFilename, " ") || strings.Contains(walletFilename, ":") {
		return errors.New("you should avoid spaces and most special characters in the filename")
	}

	if isWalletdRunning() {
		errorMessage := "Bitcoinnova-service is already running in the background.\nPlease close it via "

		if isPlatformWindows {
			errorMessage += "the task manager"
		} else if isPlatformDarwin {
			errorMessage += "the activity monitor"
		} else if isPlatformLinux {
			errorMessage += "a system monitor app"
		}
		errorMessage += "."

		return errors.New(errorMessage)
	}

	pathToLogWalletdCurrentSession := logWalletdCurrentSessionFilename
	pathToLogWalletdAllSessions := logWalletdAllSessionsFilename
	pathToWalletd := "./" + walletdCommandName
	pathToWallet := walletFilename

	pathToAppDirectory, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal("error finding current directory. Error: ", err)
	}

	if isPlatformDarwin {
		pathToAppContents := filepath.Dir(pathToAppDirectory)
		pathToWalletd = pathToAppContents + "/" + walletdCommandName

		usr, err := user.Current()
		if err != nil {
			log.Fatal("error finding home directory. Error: ", err)
		}
		pathToHomeDir := usr.HomeDir
		pathToAppLibDir := pathToHomeDir + "/Library/Application Support/BitcoinNova-Nest"

		pathToLogWalletdCurrentSession = pathToAppLibDir + "/" + logWalletdCurrentSessionFilename
		pathToLogWalletdAllSessions = pathToAppLibDir + "/" + logWalletdAllSessionsFilename
		pathToWallet = pathToHomeDir + "/" + walletFilename
	} else if isPlatformLinux {
		pathToWalletd = pathToAppDirectory + "/" + walletdCommandName
		pathToLogWalletdCurrentSession = pathToAppDirectory + "/" + logWalletdCurrentSessionFilename
		pathToLogWalletdAllSessions = pathToAppDirectory + "/" + logWalletdAllSessionsFilename
		pathToWallet = pathToAppDirectory + "/" + walletFilename
	}

	// check file with same filename does not already exist
	if _, err := os.Stat(pathToWallet); err == nil {
		return errors.New("a file with the same filename already exists")
	}

	if walletPassword != walletPasswordConfirmation {
		return errors.New("passwords do not match")
	}

	// setup current session log file (logs are added real time in this file)
	walletdCurrentSessionLogFile, err := os.Create(pathToLogWalletdCurrentSession)
	if err != nil {
		log.Error(err)
	}
	defer walletdCurrentSessionLogFile.Close()

	_, err = strconv.ParseUint(scanHeight, 10, 64)
	if err != nil || scanHeight == "" {
		scanHeight = "0"
	}

	if privateViewKey == "" && privateSpendKey == "" && mnemonicSeed == "" {
		// generate new wallet
		cmdWalletd = exec.Command(pathToWalletd, "-w", pathToWallet, "-p", walletPassword, "-l", pathToLogWalletdCurrentSession, "--log-level", walletdLogLevel, "-g")
	} else if mnemonicSeed == "" {
		// import wallet from private view and spend keys
		cmdWalletd = exec.Command(pathToWalletd, "-w", pathToWallet, "-p", walletPassword, "--view-key", privateViewKey, "--spend-key", privateSpendKey, "-l", pathToLogWalletdCurrentSession, "--log-level", walletdLogLevel, "--scan-height", scanHeight, "-g")
	} else {
		// import wallet from seed
		cmdWalletd = exec.Command(pathToWalletd, "-w", pathToWallet, "-p", walletPassword, "--mnemonic-seed", mnemonicSeed, "-l", pathToLogWalletdCurrentSession, "--log-level", walletdLogLevel, "--scan-height", scanHeight, "-g")
	}

	hideCmdWindowIfNeeded(cmdWalletd)

	// setup all sessions log file
	walletdAllSessionsLogFile, err := os.OpenFile(pathToLogWalletdAllSessions, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Fatal(err)
	}
	cmdWalletd.Stdout = walletdAllSessionsLogFile
	defer walletdAllSessionsLogFile.Close()

	err = cmdWalletd.Start()
	if err != nil {
		log.Error(err)
		return err
	}

	successCreatingWallet := false

	timesCheckLog := 0
	timeBetweenChecks := 100 * time.Millisecond
	maxWaitingTime := 5 * time.Second
	var listWalletdErrors []string

	readerWalletdLog := bufio.NewReader(walletdCurrentSessionLogFile)

	for !successCreatingWallet && time.Duration(timesCheckLog)*timeBetweenChecks < maxWaitingTime {
		timesCheckLog++
		time.Sleep(timeBetweenChecks)
		for {
			line, err := readerWalletdLog.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Error("Failed reading log file line by line: ", err)
				}
				break
			}
			if strings.Contains(line, "New wallet is generated. Address:") || strings.Contains(line, "New wallet added") {
				successCreatingWallet = true
				break
			}
			if strings.Contains(line, "INFO    Stopped") {
				errorMessage := ""

				if len(listWalletdErrors) > 0 {
					for _, line := range listWalletdErrors {
						errorMessage = errorMessage + line
					}
				} else {
					errorMessage = "Bitcoinnova-service stopped with unknown error"
				}

				killWalletd()
				return errors.New(errorMessage)
			}
			identifierErrorMessage := " ERROR  "
			if strings.Contains(line, identifierErrorMessage) {
				splitLine := strings.Split(line, identifierErrorMessage)
				listWalletdErrors = append(listWalletdErrors, splitLine[len(splitLine)-1])
			} else {
				identifierErrorMessage = "error: "
				if strings.Contains(line, identifierErrorMessage) {
					splitLine := strings.Split(line, identifierErrorMessage)
					listWalletdErrors = append(listWalletdErrors, splitLine[len(splitLine)-1])
				}
			}
		}
	}

	time.Sleep(500 * time.Millisecond)
	killWalletd()
	time.Sleep(1 * time.Second)

	return nil
}

// RequestConnectionInfo provides the blockchain sync status and the number of connected peers
func RequestConnectionInfo() (syncing string, walletBlockCount int, knownBlockCount int, localDaemonBlockCount int, peerCount int, err error) {

	walletBlockCount, knownBlockCount, localDaemonBlockCount, peerCount, err = bitcoinnovawalletdrpcgo.RequestStatus(rpcPassword)
	if err != nil {
		return "", 0, 0, 0, 0, err
	}

	stringWait := " (No transfers allowed)"
	if knownBlockCount == 0 {
		WalletdSynced = false
		syncing = "Getting block count..." + stringWait
		//} else if walletBlockCount < knownBlockCount-1 || walletBlockCount > knownBlockCount+10 || localDaemonBlockCount < knownBlockCount-1 || localDaemonBlockCount > knownBlockCount+10 {
	} else if walletBlockCount < knownBlockCount-1 || walletBlockCount > knownBlockCount+10 {
		// second condition handles cases when knownBlockCount is off and smaller than the blockCount
		WalletdSynced = false
		syncing = "Syncing..." + stringWait
	} else {
		WalletdSynced = true
		syncing = "Synced"
	}

	return syncing, walletBlockCount, knownBlockCount, localDaemonBlockCount, peerCount, nil
}

// RequestFeeinfo provides the additional fee requested by the remote node for each transaction
func RequestFeeinfo() (nodeFee float64, err error) {

	_, nodeFee, _, err = bitcoinnovawalletdrpcgo.Feeinfo(rpcPassword)
	if err != nil {
		return 0, err
	}

	NodeFee = nodeFee

	return nodeFee, nil
}

// generate a random string with n characters. from https://stackoverflow.com/a/31832326/1668837
func randStringBytesMaskImprSrc(n int) string {

	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const letterIdxBits = 6                    // 6 bits to represent a letter index
	const letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	const letterIdxMax = 63 / letterIdxBits    // # of letter indices fitting in 63 bits

	src := rand.NewSource(time.Now().UnixNano())
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}

// find process in the running processes of the system (github.com/mitchellh/go-ps)
func findProcess(key string) (int, string, error) {
	pname := ""
	pid := 0
	err := errors.New("not found")
	ps, _ := ps.Processes()

	for i := range ps {
		if ps[i].Executable() == key {
			pid = ps[i].Pid()
			pname = ps[i].Executable()
			err = nil
			break
		}
	}

	return pid, pname, err
}

func isWalletdRunning() bool {

	if _, _, err := findProcess(walletdCommandName); err == nil {
		return true
	}

	if isPlatformWindows {
		if _, _, err := findProcess(walletdCommandName + ".exe"); err == nil {
			return true
		}
	}

	return false
}

func isBitcoinnovadRunning() bool {

	if _, _, err := findProcess(bitcoinnovadCommandName); err == nil {
		return true
	}

	if isPlatformWindows {
		if _, _, err := findProcess(bitcoinnovadCommandName + ".exe"); err == nil {
			return true
		}
	}

	return false
}
