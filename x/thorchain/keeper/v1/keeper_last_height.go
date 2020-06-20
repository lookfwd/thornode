package keeperv1

import (
	"fmt"
	"strings"

	"gitlab.com/thorchain/thornode/common"
	"gitlab.com/thorchain/thornode/common/cosmos"
)

// SetLastSignedHeight save last signed height into kv store
func (k KVStore) SetLastSignedHeight(ctx cosmos.Context, height int64) error {
	lastHeight, _ := k.GetLastSignedHeight(ctx)
	if lastHeight > height {
		err := fmt.Errorf("last signed height %d is larger than %d, block height can't go backward ", lastHeight, height)
		return dbError(ctx, "", err)
	}
	store := ctx.KVStore(k.storeKey)
	key := k.GetKey(ctx, prefixLastSignedHeight, "")
	store.Set([]byte(key), k.cdc.MustMarshalBinaryBare(height))
	return nil
}

// GetLastSignedHeight get last signed height from key value store
func (k KVStore) GetLastSignedHeight(ctx cosmos.Context) (int64, error) {
	var height int64
	key := k.GetKey(ctx, prefixLastSignedHeight, "")
	store := ctx.KVStore(k.storeKey)
	if !store.Has([]byte(key)) {
		return 0, nil
	}
	buf := store.Get([]byte(key))
	if err := k.cdc.UnmarshalBinaryBare(buf, &height); err != nil {
		return 0, dbError(ctx, "Unmarshal: last heights", err)
	}
	return height, nil
}

// SetLastChainHeight save last chain height
func (k KVStore) SetLastChainHeight(ctx cosmos.Context, chain common.Chain, height int64) error {
	lastHeight, _ := k.GetLastChainHeight(ctx, chain)
	if lastHeight > height {
		err := fmt.Errorf("last block height %d is larger than %d, block height can't go backward ", lastHeight, height)
		return dbError(ctx, "", err)
	}
	store := ctx.KVStore(k.storeKey)
	key := k.GetKey(ctx, prefixLastChainHeight, chain.String())
	store.Set([]byte(key), k.cdc.MustMarshalBinaryBare(height))
	return nil
}

// GetLastChainHeight get last chain height
func (k KVStore) GetLastChainHeight(ctx cosmos.Context, chain common.Chain) (int64, error) {
	var height int64
	key := k.GetKey(ctx, prefixLastChainHeight, chain.String())
	store := ctx.KVStore(k.storeKey)
	if !store.Has([]byte(key)) {
		return 0, nil
	}
	buf := store.Get([]byte(key))
	if err := k.cdc.UnmarshalBinaryBare(buf, &height); err != nil {
		return height, dbError(ctx, "Unmarshal: last heights", err)
	}
	return height, nil
}

// GetLastChainHeights get the iterator for last chain height
func (k KVStore) GetLastChainHeights(ctx cosmos.Context) (map[common.Chain]int64, error) {
	store := ctx.KVStore(k.storeKey)
	iter := cosmos.KVStorePrefixIterator(store, []byte(prefixLastChainHeight))
	result := make(map[common.Chain]int64)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		key := string(iter.Key())
		c := strings.TrimPrefix(key, string(prefixLastChainHeight+"/"))
		chain, err := common.NewChain(c)
		if err != nil {
			return nil, fmt.Errorf("fail to parse chain: %w", err)
		}
		var height int64
		k.cdc.MustUnmarshalBinaryBare(iter.Value(), &height)
		result[chain] = height
	}
	return result, nil
}
