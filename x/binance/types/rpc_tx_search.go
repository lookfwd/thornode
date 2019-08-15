package types

type RPCTxSearch struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      string `json:"id"`
	Result  struct {
		Txs []struct {
			Hash     string `json:"hash"`
			Height   string `json:"height"`
		} `json:"txs"`
		TotalCount string `json:"total_count"`
	} `json:"result"`
}
