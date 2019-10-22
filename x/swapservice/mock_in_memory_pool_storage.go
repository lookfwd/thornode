package swapservice

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"gitlab.com/thorchain/bepswap/common"

	"gitlab.com/thorchain/bepswap/thornode/x/swapservice/types"
)

var (
	notExistPoolStakerTicker = common.Ticker("NotExistPoolStakerTicker")
	notExistStakerPoolAddr   = common.Address("4252BA642F73FA402FEF18E3CB4550E5A4A6831299D5EB7E76808C8923FC1XXX")
)

type MockInMemoryPoolStorage struct {
	store map[string]interface{}
}

// NewMockInMemoryPoolStorage
func NewMockInMemoryPoolStorage() *MockInMemoryPoolStorage {
	return &MockInMemoryPoolStorage{store: make(map[string]interface{})}
}

func (p *MockInMemoryPoolStorage) PoolExist(ctx sdk.Context, ticker common.Ticker) bool {
	_, ok := p.store[ticker.String()]
	return ok
}
func (p *MockInMemoryPoolStorage) GetPool(ctx sdk.Context, ticker common.Ticker) Pool {
	if p, ok := p.store[ticker.String()]; ok {
		return p.(Pool)
	}
	return types.NewPool()
}
func (p *MockInMemoryPoolStorage) SetPool(ctx sdk.Context, ps Pool) {
	p.store[ps.Ticker.String()] = ps
}
func (p *MockInMemoryPoolStorage) GetStakerPool(ctx sdk.Context, stakerID common.Address) (StakerPool, error) {
	if stakerID.Equals(notExistStakerPoolAddr) {
		return NewStakerPool(stakerID), errors.New("simulate error for test")
	}
	key := getKey(prefixStakerPool, stakerID.String(), getVersion(p.GetLowestActiveVersion(ctx), prefixStakerPool))
	if res, ok := p.store[key]; ok {
		return res.(StakerPool), nil
	}
	return NewStakerPool(stakerID), nil
}
func (p *MockInMemoryPoolStorage) SetStakerPool(ctx sdk.Context, stakerID common.Address, sp StakerPool) {
	key := getKey(prefixStakerPool, stakerID.String(), getVersion(p.GetLowestActiveVersion(ctx), prefixStakerPool))
	p.store[key] = sp
}
func (p *MockInMemoryPoolStorage) GetPoolStaker(ctx sdk.Context, ticker common.Ticker) (PoolStaker, error) {
	if notExistPoolStakerTicker.Equals(ticker) {
		return NewPoolStaker(ticker, sdk.ZeroUint()), errors.New("simulate error for test")
	}
	key := getKey(prefixPoolStaker, ticker.String(), getVersion(p.GetLowestActiveVersion(ctx), prefixPoolStaker))
	if res, ok := p.store[key]; ok {
		return res.(PoolStaker), nil
	}
	return NewPoolStaker(ticker, sdk.ZeroUint()), nil
}
func (p *MockInMemoryPoolStorage) SetPoolStaker(ctx sdk.Context, ticker common.Ticker, ps PoolStaker) {
	key := getKey(prefixPoolStaker, ticker.String(), getVersion(p.GetLowestActiveVersion(ctx), prefixPoolStaker))
	p.store[key] = ps
}

func (p *MockInMemoryPoolStorage) GetLowestActiveVersion(ctx sdk.Context) int {
	return 0
}

func (p *MockInMemoryPoolStorage) GetAdminConfigValue(ctx sdk.Context, key AdminConfigKey, addr sdk.AccAddress) (string, error) {
	storekey := getKey(prefixAdmin, key.String(), getVersion(p.GetLowestActiveVersion(ctx), prefixAdmin))
	ac, ok := p.store[storekey]
	if ok {
		return ac.(AdminConfig).Value, nil
	}
	return "", nil
}

func (p *MockInMemoryPoolStorage) GetAdminConfigStakerAmtInterval(ctx sdk.Context, addr sdk.AccAddress) common.Amount {
	return common.NewAmountFromFloat(100)
}

func (p *MockInMemoryPoolStorage) AddIncompleteEvents(ctx sdk.Context, event Event) {}
