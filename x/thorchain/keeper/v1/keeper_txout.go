package keeperv1

import (
	"strconv"

	cosmos "gitlab.com/thorchain/thornode/common/cosmos"
)

// AppendTxOut - append a given item to txOut
func (k KVStore) AppendTxOut(ctx cosmos.Context, height int64, item *TxOutItem) error {
	block, err := k.GetTxOut(ctx, height)
	if err != nil {
		return err
	}
	block.TxArray = append(block.TxArray, item)
	return k.SetTxOut(ctx, block)
}

// ClearTxOut - clear a given txOut
func (k KVStore) ClearTxOut(ctx cosmos.Context, height int64) error {
	block, err := k.GetTxOut(ctx, height)
	if err != nil {
		return err
	}
	block.TxArray = make([]*TxOutItem, 0)

	store := ctx.KVStore(k.storeKey)
	key := k.GetKey(ctx, prefixTxOut, strconv.FormatInt(block.Height, 10))
	buf, err := k.cdc.MarshalBinaryBare(block)
	if err != nil {
		return dbError(ctx, "fail to marshal tx out to binary", err)
	}
	store.Set([]byte(key), buf)
	return nil
}

// SetTxOut - write the given txout information to key values tore
func (k KVStore) SetTxOut(ctx cosmos.Context, blockOut *TxOut) error {
	if blockOut == nil || blockOut.IsEmpty() {
		return nil
	}
	store := ctx.KVStore(k.storeKey)
	key := k.GetKey(ctx, prefixTxOut, strconv.FormatInt(blockOut.Height, 10))
	buf, err := k.cdc.MarshalBinaryBare(blockOut)
	if err != nil {
		return dbError(ctx, "fail to marshal tx out to binary", err)
	}
	store.Set([]byte(key), buf)
	return nil
}

// GetTxOutIterator iterate tx out
func (k KVStore) GetTxOutIterator(ctx cosmos.Context) cosmos.Iterator {
	store := ctx.KVStore(k.storeKey)
	return cosmos.KVStorePrefixIterator(store, []byte(prefixTxOut))
}

// GetTxOut - write the given txout information to key values tore
func (k KVStore) GetTxOut(ctx cosmos.Context, height int64) (*TxOut, error) {
	txOut := NewTxOut(height)
	store := ctx.KVStore(k.storeKey)
	key := k.GetKey(ctx, prefixTxOut, strconv.FormatInt(height, 10))
	if !store.Has([]byte(key)) {
		return txOut, nil
	}
	buf := store.Get([]byte(key))
	if err := k.cdc.UnmarshalBinaryBare(buf, txOut); err != nil {
		return txOut, dbError(ctx, "fail to unmarshal tx out", err)
	}
	return txOut, nil
}
