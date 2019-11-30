package observer

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/thornode/common"
	stypes "gitlab.com/thorchain/thornode/x/thorchain/types"

	"gitlab.com/thorchain/thornode/bifrost/binance"
	"gitlab.com/thorchain/thornode/bifrost/config"
	"gitlab.com/thorchain/thornode/bifrost/metrics"
	"gitlab.com/thorchain/thornode/bifrost/thorclient"
	"gitlab.com/thorchain/thornode/bifrost/thorclient/types"
)

// Observer observer service
type Observer struct {
	cfg              config.Configuration
	logger           zerolog.Logger
	blockScanner     *BinanceBlockScanner
	storage          TxInStorage
	stopChan         chan struct{}
	stateChainBridge *thorclient.StateChainBridge
	m                *metrics.Metrics
	wg               *sync.WaitGroup
	errCounter       *prometheus.CounterVec
	addrMgr          *AddressManager
}

// CurrHeight : Get the Binance current block height.
// TODO: this func is a duplicate of `getBlockUrl` and `getRPCBlock`. We don't
// need two funcs that return the current block height. Consolidate and remove
// one from `mock-binance`
func binanceHeight(rpcHost string, client http.Client) (int64, error) {
	u, err := url.Parse(rpcHost)
	if err != nil {
		return 0, errors.Wrap(err, "Unable to parse dex host")
	}
	u.Path = "abci_info"

	resp, err := client.Get(u.String())
	if err != nil {
		return 0, errors.Wrap(err, "Get request failed")
	}

	defer func() {
		if err := resp.Body.Close(); nil != err {
			log.Error().Err(err).Msg("fail to close resp body")
		}
	}()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, errors.Wrap(err, "fail to read resp body")
	}

	type ABCIinfo struct {
		Jsonrpc string `json:"jsonrpc"`
		ID      string `json:"id"`
		Result  struct {
			Response struct {
				BlockHeight string `json:"last_block_height"`
			} `json:"response"`
		} `json:"result"`
	}

	var abci ABCIinfo
	if err := json.Unmarshal(data, &abci); nil != err {
		return 0, errors.Wrap(err, "failed to unmarshal")
	}

	n, _ := strconv.ParseInt(abci.Result.Response.BlockHeight, 10, 64)
	return n, nil
}

// NewObserver create a new instance of Observer
func NewObserver(cfg config.Configuration) (*Observer, error) {
	scanStorage, err := NewBinanceChanBlockScannerStorage(cfg.ObserverDbPath)
	if nil != err {
		return nil, errors.Wrap(err, "fail to create scan storage")
	}

	m, err := metrics.NewMetrics(cfg.Metric)
	if nil != err {
		return nil, errors.Wrap(err, "fail to create metric instance")
	}
	stateChainBridge, err := thorclient.NewStateChainBridge(cfg.StateChain, m)
	if nil != err {
		return nil, errors.Wrap(err, "fail to create new state chain bridge")
	}
	logger := log.Logger.With().Str("module", "observer").Logger()

	if !cfg.BlockScanner.EnforceBlockHeight {
		startBlockHeight, err := stateChainBridge.GetBinanceChainStartHeight()
		if nil != err {
			return nil, errors.Wrap(err, "fail to get start block height from statechain")
		}

		if startBlockHeight > 0 {
			cfg.BlockScanner.StartBlockHeight = int64(startBlockHeight)
			logger.Info().Int64("height", cfg.BlockScanner.StartBlockHeight).Msg("resume from last block height known by statechain")
		} else {
			client := &http.Client{}
			cfg.BlockScanner.StartBlockHeight, err = binanceHeight(cfg.BinanceHost, *client)
			if nil != err {
				return nil, errors.Wrap(err, "fail to get binance height")
			}
			logger.Info().Int64("height", cfg.BlockScanner.StartBlockHeight).Msg("Current block height is indeterminate; using current height from Binance.")
		}
	}

	addrMgr, err := NewAddressManager(cfg.StateChain.ChainHost, m)
	if nil != err {
		return nil, errors.Wrap(err, "fail to create pool address manager")
	}

	_, isTestNet := binance.IsTestNet(cfg.BinanceHost)
	blockScanner, err := NewBinanceBlockScanner(cfg.BlockScanner, scanStorage, isTestNet, addrMgr, m)
	if nil != err {
		return nil, errors.Wrap(err, "fail to create block scanner")
	}
	return &Observer{
		cfg:              cfg,
		logger:           logger,
		blockScanner:     blockScanner,
		wg:               &sync.WaitGroup{},
		stopChan:         make(chan struct{}),
		stateChainBridge: stateChainBridge,
		storage:          scanStorage,
		m:                m,
		errCounter:       m.GetCounterVec(metrics.ObserverError),
		addrMgr:          addrMgr,
	}, nil
}

func (o *Observer) Start() error {
	if err := o.stateChainBridge.EnsureNodeWhitelistedWithTimeout(); nil != err {
		o.logger.Error().Err(err).Msg("node account is not whitelisted, can't start")
		return errors.Wrap(err, "node account is not whitelisted, can't start")
	}
	if err := o.stateChainBridge.Start(); nil != err {
		o.logger.Error().Err(err).Msg("fail to start statechain bridge")
		return errors.Wrap(err, "fail to start statechain bridge")
	}
	if err := o.m.Start(); nil != err {
		o.logger.Error().Err(err).Msg("fail to start metric collector")
		return errors.Wrap(err, "fail to start metric collector")
	}
	if err := o.addrMgr.Start(); nil != err {
		o.logger.Error().Err(err).Msg("fail to start pool address manager")
		return errors.Wrap(err, "fail to start pool address manager")
	}
	o.wg.Add(1)
	go o.txinsProcessor(o.blockScanner.GetMessages(), 1)
	o.retryAllTx()
	o.wg.Add(1)
	go o.retryTxProcessor()
	o.blockScanner.Start()
	return nil
}

func (o *Observer) retryAllTx() {
	txIns, err := o.storage.GetTxInForRetry(false)
	if nil != err {
		o.logger.Error().Err(err).Msg("fail to get txin for retry")
		o.errCounter.WithLabelValues("fail_get_txin_for_retry", "").Inc()
		return
	}
	for _, item := range txIns {
		o.processOneTxIn(item)
	}
}

func (o *Observer) retryTxProcessor() {
	o.logger.Info().Msg("start retry process")
	defer o.logger.Info().Msg("stop retry process")
	defer o.wg.Done()
	// retry all
	t := time.NewTicker(o.cfg.ObserverRetryInterval)
	defer t.Stop()
	for {
		select {
		case <-o.stopChan:
			return
		case <-t.C:
			txIns, err := o.storage.GetTxInForRetry(true)
			if nil != err {
				o.errCounter.WithLabelValues("fail_to_get_txin_for_retry", "").Inc()
				o.logger.Error().Err(err).Msg("fail to get txin for retry")
				continue
			}
			for _, item := range txIns {
				select {
				case <-o.stopChan:
					return
				default:
					o.processOneTxIn(item)
				}
			}
		}
	}
}
func (o *Observer) txinsProcessor(ch <-chan types.TxIn, idx int) {
	o.logger.Info().Int("idx", idx).Msg("start to process tx in")
	defer o.logger.Info().Int("idx", idx).Msg("stop to process tx in")
	defer o.wg.Done()
	for {
		select {
		case <-o.stopChan:
			return
		case txIn, more := <-ch:
			if !more {
				// channel closed
				return
			}
			if len(txIn.TxArray) == 0 {
				o.logger.Debug().Msg("nothing need to forward to statechain")
				continue
			}
			o.processOneTxIn(txIn)
		}
	}
}
func (o *Observer) processOneTxIn(txIn types.TxIn) {
	if err := o.storage.SetTxInStatus(txIn, types.Processing); nil != err {
		o.errCounter.WithLabelValues("fail_save_txin_local", txIn.BlockHeight).Inc()
		o.logger.Error().Err(err).Msg("fail to save TxIn to local store")
		return
	}
	if err := o.signAndSendToStatechain(txIn); nil != err {
		o.logger.Error().Err(err).Msg("fail to send to statechain")
		o.errCounter.WithLabelValues("fail_send_to_statechain", txIn.BlockHeight).Inc()
		if err := o.storage.SetTxInStatus(txIn, types.Failed); nil != err {
			o.logger.Error().Err(err).Msg("fail to save TxIn to local store")
			return
		}
	}
	if err := o.storage.RemoveTxIn(txIn); nil != err {
		o.errCounter.WithLabelValues("fail_remove_from_local_store", txIn.BlockHeight).Inc()
		o.logger.Error().Err(err).Msg("fail to remove txin from local store")
		return
	}
}
func (o *Observer) signAndSendToStatechain(txIn types.TxIn) error {
	txs, err := o.getStateChainTxIns(txIn)
	if nil != err {
		return errors.Wrap(err, "fail to convert txin to statechain txin")
	}
	signed, err := o.stateChainBridge.Sign(txs)
	if nil != err {
		o.errCounter.WithLabelValues("fail_to_sign", txIn.BlockHeight).Inc()
		return errors.Wrap(err, "fail to sign the tx")
	}
	txID, err := o.stateChainBridge.Send(*signed, types.TxSync)
	if nil != err {
		o.errCounter.WithLabelValues("fail_to_send_to_statechain", txIn.BlockHeight).Inc()
		return errors.Wrap(err, "fail to send the tx to statechain")
	}
	o.logger.Info().Str("block", txIn.BlockHeight).Str("statechain hash", txID.String()).Msg("sign and send to statechain successfully")
	return nil
}

// getStateChainTxIns convert to the type statechain expected
// maybe in later we can just refactor this to use the type in statechain
func (o *Observer) getStateChainTxIns(txIn types.TxIn) ([]stypes.TxInVoter, error) {
	txs := make([]stypes.TxInVoter, len(txIn.TxArray))
	for i, item := range txIn.TxArray {
		o.logger.Debug().Str("tx-hash", item.Tx).Msg("txInItem")
		txID, err := common.NewTxID(item.Tx)
		if nil != err {
			o.errCounter.WithLabelValues("fail_to_parse_tx_hash", txIn.BlockHeight).Inc()
			return nil, errors.Wrapf(err, "fail to parse tx hash, %s is invalid ", item.Tx)
		}
		sender, err := common.NewAddress(item.Sender)
		if nil != err {
			o.errCounter.WithLabelValues("fail_to_parse_sender", item.Sender).Inc()
			return nil, errors.Wrapf(err, "fail to parse sender,%s is invalid sender address", item.Sender)
		}

		to, err := common.NewAddress(item.To)
		if nil != err {
			o.errCounter.WithLabelValues("fail_to_parse_sender", item.Sender).Inc()
			return nil, errors.Wrapf(err, "fail to parse sender,%s is invalid sender address", item.Sender)
		}

		h, err := strconv.ParseUint(txIn.BlockHeight, 10, 64)
		if nil != err {
			o.errCounter.WithLabelValues("fail to parse block height", txIn.BlockHeight).Inc()
			return nil, errors.Wrapf(err, "fail to parse block height")
		}
		observedPoolPubKey, err := common.NewPubKey(item.ObservedPoolAddress)
		if nil != err {
			o.errCounter.WithLabelValues("fail to parse observed pool address", item.ObservedPoolAddress).Inc()
			return nil, errors.Wrapf(err, "fail to parse observed pool address: %s", item.ObservedPoolAddress)
		}
		txs[i] = stypes.NewTxInVoter(txID, []stypes.TxIn{
			stypes.NewTxIn(
				item.Coins,
				item.Memo,
				sender,
				to,
				common.BNBGasFeeSingleton,
				sdk.NewUint(h),
				observedPoolPubKey),
		})
	}
	return txs, nil
}

// Stop the observer
func (o *Observer) Stop() error {
	o.logger.Debug().Msg("request to stop observer")
	defer o.logger.Debug().Msg("observer stopped")

	if err := o.blockScanner.Stop(); nil != err {
		o.logger.Error().Err(err).Msg("fail to close block scanner")
	}

	close(o.stopChan)
	o.wg.Wait()
	if err := o.addrMgr.Stop(); nil != err {
		o.logger.Error().Err(err).Msg("fail to stop pool address manager")
	}
	return o.m.Stop()
}