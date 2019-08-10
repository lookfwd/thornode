package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type status string

const (
	Incomplete status = "incomplete"
	Done       status = "done"
	Reverted   status = "reverted"
)

// Meant to track if we have processed a specific binance tx
type TxHash struct {
	Request TxID       `json:"request"` // binance chain request tx hash
	Status  status     `json:"status"`
	Done    TxID       `json:"txhash"` // completed binance chain tx hash
	Memo    string     `json:"memo"`   // memo
	Coins   sdk.Coins  `json:"coins"`  // coins sent in tx
	Sender  BnbAddress `json:"sender"`
}

func NewTxHash(hash TxID, coins sdk.Coins, memo string, sender BnbAddress) TxHash {
	return TxHash{
		Request: hash,
		Coins:   coins,
		Memo:    memo,
		Sender:  sender,
		Status:  Incomplete,
	}
}

func (tx TxHash) Empty() bool {
	return tx.Request.Empty()
}

func (tx TxHash) String() string {
	return tx.Request.String()
}

// Generate db key for kvstore
func (tx TxHash) Key() string {
	return tx.Request.String()
}

func (tx *TxHash) SetDone(hash TxID) {
	tx.Status = Done
	tx.Done = hash
}

func (tx *TxHash) SetReverted(hash TxID) {
	tx.Status = Reverted
	tx.Done = hash
}
