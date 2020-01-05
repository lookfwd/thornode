package signer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/thornode/bifrost/blockscanner"
	"gitlab.com/thorchain/thornode/bifrost/config"
	"gitlab.com/thorchain/thornode/bifrost/metrics"
	"gitlab.com/thorchain/thornode/bifrost/thorclient"
	stypes "gitlab.com/thorchain/thornode/bifrost/thorclient/types"
	"gitlab.com/thorchain/thornode/common"
)

type ThorchainBlockScan struct {
	logger             zerolog.Logger
	wg                 *sync.WaitGroup
	stopChan           chan struct{}
	txOutChan          chan stypes.TxOut
	keygensChan        chan stypes.Keygens
	cfg                config.BlockScannerConfiguration
	scannerStorage     blockscanner.ScannerStorage
	commonBlockScanner *blockscanner.CommonBlockScanner
	thorchain          *thorclient.ThorchainBridge
	m                  *metrics.Metrics
	errCounter         *prometheus.CounterVec
	pkm                *PubKeyManager
	cdc                *codec.Codec
}

// NewThorchainBlockScan create a new instance of thorchain block scanner
func NewThorchainBlockScan(cfg config.BlockScannerConfiguration, scanStorage blockscanner.ScannerStorage, thorchain *thorclient.ThorchainBridge, m *metrics.Metrics, pkm *PubKeyManager) (*ThorchainBlockScan, error) {
	if nil == scanStorage {
		return nil, errors.New("scanStorage is nil")
	}
	if nil == m {
		return nil, errors.New("metric is nil")
	}
	commonBlockScanner, err := blockscanner.NewCommonBlockScanner(cfg, scanStorage, m)
	if nil != err {
		return nil, errors.Wrap(err, "fail to create txOut block scanner")
	}
	return &ThorchainBlockScan{
		logger:             log.With().Str("module", "thorchainblockscanner").Logger(),
		wg:                 &sync.WaitGroup{},
		stopChan:           make(chan struct{}),
		txOutChan:          make(chan stypes.TxOut),
		keygensChan:        make(chan stypes.Keygens),
		cfg:                cfg,
		scannerStorage:     scanStorage,
		commonBlockScanner: commonBlockScanner,
		thorchain:          thorchain,
		errCounter:         m.GetCounterVec(metrics.ThorchainBlockScanError),
		pkm:                pkm,
		cdc:                codec.New(),
	}, nil
}

// GetMessages return the channel
func (b *ThorchainBlockScan) GetTxOutMessages() <-chan stypes.TxOut {
	return b.txOutChan
}

func (b *ThorchainBlockScan) GetKeygenMessages() <-chan stypes.Keygens {
	return b.keygensChan
}

// Start to scan blocks
func (b *ThorchainBlockScan) Start() error {
	b.wg.Add(1)
	go b.processBlocks(1)
	b.commonBlockScanner.Start()
	return nil
}

func (b *ThorchainBlockScan) processKeygenBlock(blockHeight int64) error {
	for _, pk := range b.pkm.pks {
		uri := b.thorchain.GetUrl(fmt.Sprintf("/thorchain/keygen/%d/%s", blockHeight, pk.String()))

		strBlockHeight := strconv.FormatInt(blockHeight, 10)
		buf, err := b.commonBlockScanner.GetFromHttpWithRetry(uri)
		if nil != err {
			b.errCounter.WithLabelValues("fail_get_keygen", strBlockHeight)
			return errors.Wrap(err, "fail to get keygen from a block")
		}

		var keygens stypes.Keygens
		if err := b.cdc.UnmarshalJSON(buf, &keygens); err != nil {
			b.errCounter.WithLabelValues("fail_unmarshal_keygens", strBlockHeight)
			return errors.Wrap(err, "fail to unmarshal keygens")
		}
		b.keygensChan <- keygens
	}
	return nil
}

func (b *ThorchainBlockScan) processTxOutBlock(blockHeight int64) error {
	for _, pk := range b.pkm.pks {
		if len(pk.String()) == 0 {
			continue
		}
		uri := b.thorchain.GetUrl(fmt.Sprintf("/thorchain/keysign/%d/%s", blockHeight, pk.String()))
		strBlockHeight := strconv.FormatInt(blockHeight, 10)
		buf, err := b.commonBlockScanner.GetFromHttpWithRetry(uri)
		if nil != err {
			b.errCounter.WithLabelValues("fail_get_tx_out", strBlockHeight)
			return errors.Wrap(err, "fail to get tx out from a block")
		}

		type txOut struct {
			Chains map[common.Chain]stypes.TxOut `json:"chains"`
		}

		var tx txOut
		if err := json.Unmarshal(buf, &tx); err != nil {
			b.errCounter.WithLabelValues("fail_unmarshal_tx_out", strBlockHeight)
			return errors.Wrap(err, "fail to unmarshal TxOut")
		}
		for c, out := range tx.Chains {
			b.logger.Debug().Str("chain", c.String()).Msg("chain")
			if len(out.TxArray) == 0 {
				b.logger.Debug().Int64("block", blockHeight).Msg("nothing to process")
				b.m.GetCounter(metrics.BlockNoTxOut).Inc()
				return nil
			}
			// TODO here THORNode will need to dispatch to different chain processor
			b.txOutChan <- out
		}
	}
	return nil
}

func (b *ThorchainBlockScan) processBlocks(idx int) {
	b.logger.Debug().Int("idx", idx).Msg("start searching tx out in a block")
	defer b.logger.Debug().Int("idx", idx).Msg("stop searching tx out in a block")
	defer b.wg.Done()

	for {
		select {
		case <-b.stopChan: // time to get out
			return
		case block, more := <-b.commonBlockScanner.GetMessages():
			if !more {
				return
			}
			b.logger.Debug().Int64("block", block).Msg("processing block")
			if err := b.processTxOutBlock(block); nil != err {
				if errStatus := b.scannerStorage.SetBlockScanStatus(block, blockscanner.Failed); nil != errStatus {
					b.errCounter.WithLabelValues("fail_set_block_Status", strconv.FormatInt(block, 10))
					b.logger.Error().Err(err).Int64("height", block).Msg("fail to set block to fail status")
				}
				b.errCounter.WithLabelValues("fail_search_tx", strconv.FormatInt(block, 10))
				b.logger.Error().Err(err).Int64("height", block).Msg("fail to search tx in block")
				// THORNode will have a retry go routine to check it.
				continue
			}

			// set a block as success
			if err := b.scannerStorage.RemoveBlockStatus(block); nil != err {
				b.errCounter.WithLabelValues("fail_remove_block_Status", strconv.FormatInt(block, 10))
				b.logger.Error().Err(err).Int64("block", block).Msg("fail to remove block status from data store, thus block will be re processed")
			}

			// Intentionally not covering this before the block is marked as
			// success. This is because we don't care if keygen is successful
			// or not.
			b.logger.Debug().Int64("block", block).Msg("processing keygen block")
			if err := b.processKeygenBlock(block); nil != err {
				b.errCounter.WithLabelValues("fail_process_keygen", strconv.FormatInt(block, 10))
				b.logger.Error().Err(err).Int64("height", block).Msg("fail to process keygen")
			}
		}
	}
}

// Stop the scanner
func (b *ThorchainBlockScan) Stop() error {
	b.logger.Info().Msg("received request to stop thorchain block scanner")
	defer b.logger.Info().Msg("thorchain block scanner stopped successfully")
	close(b.stopChan)
	b.wg.Wait()
	return nil
}