package swapservice

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/libs/log"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/statechain/x/swapservice/mocks"
)

func Test(t *testing.T) { TestingT(t) }

type RefundSuite struct{}

var _ = Suite(&RefundSuite{})

func getTestContext() sdk.Context {
	key := sdk.NewKVStoreKey("test")
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db)
	cms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	cms.LoadLatestVersion()
	return sdk.NewContext(cms, abci.Header{}, false, log.NewNopLogger())

}
func newPoolStructForTest(ticker Ticker, balanceRune, balanceToken Amount) PoolStruct {
	ps := NewPoolStruct()
	ps.BalanceToken = balanceToken
	ps.BalanceRune = balanceRune
	ps.Ticker = ticker
	return ps
}
func (*RefundSuite) TestGetRefundCoin(c *C) {

	refundStoreAccessor := mocks.NewMockRefundStoreAccessor()
	bnbTicker, err := NewTicker("BNB")
	c.Assert(err, IsNil)
	inputs := []struct {
		name                string
		minimumRefundAmount Amount
		poolStruct          PoolStruct
		ticker              Ticker
		amount              Amount
		expectedCoin        Coin
	}{
		{
			name:                "invalid-MRRA",
			minimumRefundAmount: Amount("invalid"),
			poolStruct:          newPoolStructForTest(RuneTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			ticker:              RuneTicker,
			amount:              NewAmountFromFloat(100),
			expectedCoin:        NewCoin(RuneTicker, NewAmountFromFloat(100)),
		},
		{
			name:                "OneRune-MRRA",
			minimumRefundAmount: NewAmountFromFloat(1.0),
			poolStruct:          newPoolStructForTest(RuneTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			ticker:              RuneTicker,
			amount:              NewAmountFromFloat(100),
			expectedCoin:        NewCoin(RuneTicker, NewAmountFromFloat(99)),
		},
		{
			name:                "No-Refund",
			minimumRefundAmount: NewAmountFromFloat(1.0),
			poolStruct:          newPoolStructForTest(RuneTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			ticker:              RuneTicker,
			amount:              NewAmountFromFloat(0.5),
			expectedCoin:        NewCoin(RuneTicker, ZeroAmount),
		},
		{
			name:                "invalid-MRRA-BNB-refund-all",
			minimumRefundAmount: Amount("invalid"),
			poolStruct:          newPoolStructForTest(bnbTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			ticker:              bnbTicker,
			amount:              NewAmountFromFloat(5),
			expectedCoin:        NewCoin(bnbTicker, NewAmountFromFloat(5)),
		},
		{
			name:                "MRRA-BNB-refund-normal",
			minimumRefundAmount: NewAmountFromFloat(1.0),
			poolStruct:          newPoolStructForTest(bnbTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			ticker:              bnbTicker,
			amount:              NewAmountFromFloat(5),
			expectedCoin:        NewCoin(bnbTicker, NewAmountFromFloat(4)),
		},
		{
			name:                "MRRA-BNB-no-refund",
			minimumRefundAmount: NewAmountFromFloat(1.0),
			poolStruct:          newPoolStructForTest(bnbTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			ticker:              bnbTicker,
			amount:              NewAmountFromFloat(0.5),
			expectedCoin:        NewCoin(bnbTicker, ZeroAmount),
		},
	}
	for _, item := range inputs {
		ctx := getTestContext()
		ctx = ctx.WithValue(mocks.RefundAdminConfigKey, item.minimumRefundAmount).
			WithValue(mocks.RefundPoolStructKey, item.poolStruct)
		coin := getRefundCoin(ctx, item.ticker, item.amount, refundStoreAccessor)
		c.Assert(coin, Equals, item.expectedCoin)
	}
}

// TestProcessRefund is to test the processRefund
func (*RefundSuite) TestProcessRefund(c *C) {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("rune", "runepub")
	refundStoreAccessor := mocks.NewMockRefundStoreAccessor()
	bnbTicker, err := NewTicker("BNB")
	c.Assert(err, IsNil)
	accountAddress, err := sdk.AccAddressFromBech32("rune1lz8kde0dc5ru63et7kykzzc97jhu7rg3yp2qxd")
	c.Assert(err, IsNil)
	txID, err := NewTxID("A1C7D97D5DB51FFDBC3FE29FFF6ADAA2DAF112D2CEAADA0902822333A59BD218")
	c.Assert(err, IsNil)
	inputs := []struct {
		name                string
		minimumRefundAmount Amount
		poolStruct          PoolStruct
		result              sdk.Result
		msg                 sdk.Msg
		out                 *TxOutItem
	}{
		{
			name:                "result-ok",
			minimumRefundAmount: NewAmountFromFloat(1.0),
			poolStruct:          newPoolStructForTest(bnbTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			result: sdk.Result{
				Code: sdk.CodeOK,
			},
			msg: nil,
			out: nil,
		},
		{
			name:                "msg-type-setpooldata",
			minimumRefundAmount: NewAmountFromFloat(1.0),
			poolStruct:          newPoolStructForTest(bnbTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			result: sdk.Result{
				Code: sdk.CodeOK,
			},
			msg: NewMsgSetPoolData(bnbTicker, PoolEnabled, accountAddress),
			out: nil,
		},
		{
			name:                "msg-type-swap",
			minimumRefundAmount: NewAmountFromFloat(1.0),
			poolStruct:          newPoolStructForTest(bnbTicker, NewAmountFromFloat(100), NewAmountFromFloat(100)),
			result:              sdk.ErrUnknownRequest("whatever").Result(),
			msg:                 NewMsgSwap(txID, RuneTicker, bnbTicker, NewAmountFromFloat(5.0), "asdf", "asdf", "1.0", accountAddress),
			out: &TxOutItem{
				ToAddress: "asdf",
				Coins: []Coin{
					NewCoin(RuneTicker, NewAmountFromFloat(4.0)),
				},
			},
		},
	}
	for _, item := range inputs {
		ctx := getTestContext()
		ctx = ctx.WithValue(mocks.RefundAdminConfigKey, item.minimumRefundAmount).
			WithValue(mocks.RefundPoolStructKey, item.poolStruct)
		txStore := &TxOutStore{
			blockOut: nil,
		}
		txStore.NewBlock(1)
		processRefund(ctx, &item.result, txStore, refundStoreAccessor, item.msg)
		if nil == item.out {
			c.Assert(txStore.blockOut.TxArray, IsNil)
		} else {
			if len(txStore.blockOut.TxArray) == 0 {
				c.FailNow()
			}
			c.Assert(item.out.String(), Equals, txStore.blockOut.TxArray[0].String())
		}
	}
}