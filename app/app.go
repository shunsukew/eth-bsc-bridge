package app

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/ChainSafe/chainbridge-core/chains/evm"
	"github.com/ChainSafe/chainbridge-core/chains/evm/evmclient"
	"github.com/ChainSafe/chainbridge-core/chains/evm/evmtransaction"
	"github.com/ChainSafe/chainbridge-core/chains/evm/listener"
	"github.com/ChainSafe/chainbridge-core/chains/evm/voter"
	"github.com/ChainSafe/chainbridge-core/config"
	"github.com/ChainSafe/chainbridge-core/lvldb"
	"github.com/ChainSafe/chainbridge-core/relayer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

func Run() error {
	errChn := make(chan error)
	stopChn := make(chan struct{})

	db, err := lvldb.NewLvlDB(viper.GetString(config.BlockstoreFlagName))
	if err != nil {
		panic(err)
	}

	// ===== Eth setup =====
	ethClient := evmclient.NewEVMClient()
	err = ethClient.Configurate(viper.GetString(config.ConfigFlagName), "config_eth.json")
	if err != nil {
		panic(err)
	}

	ethConfig := ethClient.GetConfig()
	ethEventHandler := listener.NewETHEventHandler(common.HexToAddress(ethConfig.SharedEVMConfig.Bridge), ethClient) // <- function name should be NewEVMEventHandler
	// register erc20 event handler. chainbridge-core now support only erc20handler. if you want to add erc721, you need to add another event handler here.
	ethEventHandler.RegisterEventHandler(ethConfig.SharedEVMConfig.Erc20Handler, listener.Erc20EventHandler)
	ethListener := listener.NewEVMListener(ethClient, ethEventHandler, common.HexToAddress(ethConfig.SharedEVMConfig.Bridge))

	ethMessageHandler := voter.NewEVMMessageHandler(ethClient, common.HexToAddress(ethConfig.SharedEVMConfig.Bridge))
	// register erc20 message handler. chainbridge-core now support only erc20handler. if you want to add erc721, you need to add another message handler here.
	ethMessageHandler.RegisterMessageHandler(common.HexToAddress(ethConfig.SharedEVMConfig.Erc20Handler), voter.ERC20MessageHandler)
	ethVoter := voter.NewVoter(ethMessageHandler, ethClient, evmtransaction.NewTransaction)

	ethChain := evm.NewEVMChain(ethListener, ethVoter, db, *ethConfig.SharedEVMConfig.GeneralChainConfig.Id, &ethConfig.SharedEVMConfig)

	// ===== Bsc setup =====
	// bsc is evm compatible. so we can utilize evm module
	bscClient := evmclient.NewEVMClient()
	err = bscClient.Configurate(viper.GetString(config.ConfigFlagName), "config_bsc.json")
	if err != nil {
		panic(err)
	}

	bscConfig := bscClient.GetConfig()
	bscEventHandler := listener.NewETHEventHandler(common.HexToAddress(bscConfig.SharedEVMConfig.Bridge), bscClient)
	bscEventHandler.RegisterEventHandler(bscConfig.SharedEVMConfig.Erc20Handler, listener.Erc20EventHandler)
	bscListener := listener.NewEVMListener(bscClient, bscEventHandler, common.HexToAddress(bscConfig.SharedEVMConfig.Bridge))

	bscMessageHandler := voter.NewEVMMessageHandler(bscClient, common.HexToAddress(bscConfig.SharedEVMConfig.Bridge))
	bscMessageHandler.RegisterMessageHandler(common.HexToAddress(bscConfig.SharedEVMConfig.Erc20Handler), voter.ERC20MessageHandler)
	bscVoter := voter.NewVoter(bscMessageHandler, bscClient, evmtransaction.NewTransaction)

	bscChain := evm.NewEVMChain(bscListener, bscVoter, db, *bscConfig.SharedEVMConfig.GeneralChainConfig.Id, &bscConfig.SharedEVMConfig)

	r := relayer.NewRelayer([]relayer.RelayedChain{ethChain, bscChain})

	go r.Start(stopChn, errChn)

	sysErr := make(chan os.Signal, 1)
	signal.Notify(sysErr,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGHUP,
		syscall.SIGQUIT)

	select {
	case err := <-errChn:
		log.Error().Err(err).Msg("failed to listen and serve")
		close(stopChn)
		return err
	case sig := <-sysErr:
		log.Info().Msgf("terminating got ` [%v] signal", sig)
		return nil
	}
}
