// Copyright (c) 2018, The Bitcoin Nova Developers
//
// Please see the included LICENSE file for more information.
//

package walletdmanager

const (
	// DefaultTransferFee is the default fee. It is expressed in BTN
	DefaultTransferFee float64 = 0.01

	logWalletdCurrentSessionFilename     = "Bitcoinnova-service-session.log"
	logWalletdAllSessionsFilename        = "Bitcoinnova-service.log"
	logBitcoinnovadCurrentSessionFilename = "Bitcoinnovad-session.log"
	logBitcoinnovadAllSessionsFilename    = "Bitcoinnovad.log"
	walletdLogLevel                      = "3" // should be at least 3 as I use some logs messages to confirm creation of wallet
	walletdCommandName                   = "Bitcoinnova-service"
	bitcoinnovadCommandName               = "Bitcoinnovad"
)
