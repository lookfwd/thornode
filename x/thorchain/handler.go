package thorchain

import (
	"encoding/json"
	"fmt"

	"github.com/blang/semver"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"

	"gitlab.com/thorchain/thornode/common"
	"gitlab.com/thorchain/thornode/constants"
)

// THORChain error code start at 101
const (
	// CodeBadVersion error code for bad version
	CodeBadVersion     sdk.CodeType = 101
	CodeInvalidMessage sdk.CodeType = 102
)

// EmptyAccAddress empty address
var EmptyAccAddress = sdk.AccAddress{}
var notAuthorized = fmt.Errorf("not authorized")
var badVersion = fmt.Errorf("bad version")
var errBadVersion = sdk.NewError(DefaultCodespace, CodeBadVersion, "bad version")
var errInvalidMessage = sdk.NewError(DefaultCodespace, CodeInvalidMessage, "invalid message")

// NewHandler returns a handler for "thorchain" type messages.
func NewHandler(keeper Keeper, poolAddrMgr PoolAddressManager, txOutStore TxOutStore, validatorMgr ValidatorManager) sdk.Handler {
	// Classic Handler
	classic := NewClassicHandler(keeper, poolAddrMgr, txOutStore, validatorMgr)
	handlerMap := getHandlerMapping(keeper, poolAddrMgr, txOutStore, validatorMgr)

	return func(ctx sdk.Context, msg sdk.Msg) sdk.Result {
		version := keeper.GetLowestActiveVersion(ctx)
		h, ok := handlerMap[msg.Type()]
		if !ok {
			return classic(ctx, msg)
		}
		return h.Run(ctx, msg, version)
	}
}

func getHandlerMapping(keeper Keeper, poolAddrMgr PoolAddressManager, txOutStore TxOutStore, validatorMgr ValidatorManager) map[string]MsgHandler {
	// New arch handlers
	m := make(map[string]MsgHandler)
	m[MsgNoOp{}.Type()] = NewNoOpHandler(keeper)
	m[MsgYggdrasil{}.Type()] = NewYggdrasilHandler(keeper, txOutStore, poolAddrMgr, validatorMgr)
	m[MsgReserveContributor{}.Type()] = NewReserveContributorHandler(keeper)
	m[MsgSetPoolData{}.Type()] = NewPoolDataHandler(keeper)
	m[MsgSetVersion{}.Type()] = NewVersionHandler(keeper)
	m[MsgBond{}.Type()] = NewBondHandler(keeper)
	m[MsgObservedTxIn{}.Type()] = NewObservedTxInHandler(keeper, txOutStore, poolAddrMgr, validatorMgr)
	m[MsgObservedTxOut{}.Type()] = NewObservedTxOutHandler(keeper, txOutStore, poolAddrMgr, validatorMgr)
	m[MsgLeave{}.Type()] = NewLeaveHandler(keeper, validatorMgr, poolAddrMgr, txOutStore)
	m[MsgAdd{}.Type()] = NewAddHandler(keeper)
	m[MsgSetUnStake{}.Type()] = NewUnstakeHandler(keeper, txOutStore, poolAddrMgr)
	m[MsgSetStakeData{}.Type()] = NewStakeHandler(keeper)
	return m
}

// NewClassicHandler returns a handler for "thorchain" type messages.
func NewClassicHandler(keeper Keeper, poolAddressMgr PoolAddressManager, txOutStore TxOutStore, validatorManager ValidatorManager) sdk.Handler {
	return func(ctx sdk.Context, msg sdk.Msg) sdk.Result {
		switch m := msg.(type) {
		case MsgSwap:
			return handleMsgSwap(ctx, keeper, txOutStore, poolAddressMgr, m)
		case MsgSetAdminConfig:
			return handleMsgSetAdminConfig(ctx, keeper, m)
		case MsgOutboundTx:
			return handleMsgOutboundTx(ctx, keeper, poolAddressMgr, m)
		case MsgEndPool:
			return handleOperatorMsgEndPool(ctx, keeper, txOutStore, poolAddressMgr, m)
		case MsgSetTrustAccount:
			return handleMsgSetTrustAccount(ctx, keeper, m)
		default:
			errMsg := fmt.Sprintf("Unrecognized thorchain Msg type: %v", m)
			return sdk.ErrUnknownRequest(errMsg).Result()
		}
	}
}

// handleOperatorMsgEndPool operators decide it is time to end the pool
func handleOperatorMsgEndPool(ctx sdk.Context, keeper Keeper, txOutStore TxOutStore, poolAddrMgr PoolAddressManager, msg MsgEndPool) sdk.Result {
	if !isSignedByActiveNodeAccounts(ctx, keeper, msg.GetSigners()) {
		ctx.Logger().Error("message signed by unauthorized account", "asset", msg.Asset)
		return sdk.ErrUnauthorized("Not authorized").Result()
	}
	ctx.Logger().Info("handle MsgEndPool", "asset", msg.Asset, "requester", msg.Tx.FromAddress, "signer", msg.Signer.String())
	poolStaker, err := keeper.GetPoolStaker(ctx, msg.Asset)
	if nil != err {
		ctx.Logger().Error("fail to get pool staker", err)
		return sdk.ErrInternal(err.Error()).Result()
	}

	// everyone withdraw
	for _, item := range poolStaker.Stakers {
		unstakeMsg := NewMsgSetUnStake(
			msg.Tx,
			item.RuneAddress,
			sdk.NewUint(10000),
			msg.Asset,
			msg.Signer,
		)
		unstakeHandler := NewUnstakeHandler(keeper, txOutStore, poolAddrMgr)
		ver := semver.MustParse("0.1.0")
		result := unstakeHandler.Run(ctx, unstakeMsg, ver)
		if !result.IsOK() {
			ctx.Logger().Error("fail to unstake", "staker", item.RuneAddress)
			return result
		}
	}
	pool, err := keeper.GetPool(ctx, msg.Asset)
	pool.Status = PoolSuspended
	if err := keeper.SetPool(ctx, pool); err != nil {
		err = errors.Wrap(err, "fail to set pool")
		return sdk.ErrInternal(err.Error()).Result()
	}
	return sdk.Result{
		Code:      sdk.CodeOK,
		Codespace: DefaultCodespace,
	}
}

// Handle a message to set stake data
func handleMsgSwap(ctx sdk.Context, keeper Keeper, txOutStore TxOutStore, poolAddrMgr PoolAddressManager, msg MsgSwap) sdk.Result {
	if !isSignedByActiveObserver(ctx, keeper, msg.GetSigners()) {
		ctx.Logger().Error("message signed by unauthorized account", "request tx hash", msg.Tx.ID, "source asset", msg.Tx.Coins[0].Asset, "target asset", msg.TargetAsset)
		return sdk.ErrUnauthorized("Not authorized").Result()
	}
	gsl := sdk.NewUint(constants.GlobalSlipLimit)
	chain := msg.TargetAsset.Chain
	currentAddr := poolAddrMgr.GetCurrentPoolAddresses().Current.GetByChain(chain)
	if nil == currentAddr {
		msg := fmt.Sprintf("don't have pool address for chain : %s", chain)
		ctx.Logger().Error(msg)
		return sdk.ErrInternal(msg).Result()
	}
	amount, err := swap(
		ctx,
		keeper,
		msg.Tx,
		msg.TargetAsset,
		msg.Destination,
		msg.TradeTarget,
		gsl,
	) // If so, set the stake data to the value specified in the msg.
	if err != nil {
		ctx.Logger().Error("fail to process swap message", "error", err)

		return sdk.ErrInternal(err.Error()).Result()
	}

	res, err := keeper.Cdc().MarshalBinaryLengthPrefixed(struct {
		Asset sdk.Uint `json:"asset"`
	}{
		Asset: amount,
	})
	if nil != err {
		ctx.Logger().Error("fail to encode result to json", "error", err)
		return sdk.ErrInternal("fail to encode result to json").Result()
	}

	toi := &TxOutItem{
		Chain:       currentAddr.Chain,
		InHash:      msg.Tx.ID,
		VaultPubKey: currentAddr.PubKey,
		ToAddress:   msg.Destination,
		Coin:        common.NewCoin(msg.TargetAsset, amount),
	}
	txOutStore.AddTxOutItem(ctx, toi)
	return sdk.Result{
		Code:      sdk.CodeOK,
		Data:      res,
		Codespace: DefaultCodespace,
	}
}

func processOneTxIn(ctx sdk.Context, keeper Keeper, tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	if len(tx.Tx.Coins) == 0 {
		return nil, fmt.Errorf("no coin found")
	}
	memo, err := ParseMemo(tx.Tx.Memo)
	if err != nil {
		return nil, errors.Wrap(err, "fail to parse memo")
	}
	// THORNode should not have one tx across chain, if it is cross chain it should be separate tx
	var newMsg sdk.Msg
	// interpret the memo and initialize a corresponding msg event
	switch m := memo.(type) {
	case CreateMemo:
		newMsg, err = getMsgSetPoolDataFromMemo(ctx, keeper, m, signer)
		if nil != err {
			return nil, errors.Wrap(err, "fail to get MsgSetPoolData from memo")
		}

	case StakeMemo:
		newMsg, err = getMsgStakeFromMemo(ctx, m, tx, signer)
		if nil != err {
			return nil, errors.Wrap(err, "fail to get MsgStake from memo")
		}

	case WithdrawMemo:
		newMsg, err = getMsgUnstakeFromMemo(m, tx, signer)
		if nil != err {
			return nil, errors.Wrap(err, "fail to get MsgUnstake from memo")
		}
	case SwapMemo:
		newMsg, err = getMsgSwapFromMemo(m, tx, signer)
		if nil != err {
			return nil, errors.Wrap(err, "fail to get MsgSwap from memo")
		}
	case AddMemo:
		newMsg, err = getMsgAddFromMemo(m, tx, signer)
		if err != nil {
			return nil, errors.Wrap(err, "fail to get MsgAdd from memo")
		}
	case GasMemo:
		newMsg, err = getMsgNoOpFromMemo(tx, signer)
		if err != nil {
			return nil, errors.Wrap(err, "fail to get MsgNoOp from memo")
		}
	case OutboundMemo:
		newMsg, err = getMsgOutboundFromMemo(m, tx, signer)
		if nil != err {
			return nil, errors.Wrap(err, "fail to get MsgOutbound from memo")
		}
	case BondMemo:
		newMsg, err = getMsgBondFromMemo(m, tx, signer)
		if nil != err {
			return nil, errors.Wrap(err, "fail to get MsgBond from memo")
		}
	case LeaveMemo:
		newMsg = NewMsgLeave(tx.Tx, signer)
	case YggdrasilFundMemo:
		newMsg = NewMsgYggdrasil(tx.ObservedPubKey, true, tx.Tx.Coins, tx.Tx.ID, signer)
	case YggdrasilReturnMemo:
		newMsg = NewMsgYggdrasil(tx.ObservedPubKey, false, tx.Tx.Coins, tx.Tx.ID, signer)
	case ReserveMemo:
		res := NewReserveContributor(tx.Tx.FromAddress, tx.Tx.Coins[0].Amount)
		newMsg = NewMsgReserveContributor(res, signer)
	default:
		return nil, errors.Wrap(err, "Unable to find memo type")
	}

	if err := newMsg.ValidateBasic(); nil != err {
		return nil, errors.Wrap(err, "invalid msg")
	}
	return newMsg, nil
}

func getMsgNoOpFromMemo(tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	for _, coin := range tx.Tx.Coins {
		if !coin.Asset.IsBNB() {
			return nil, errors.New("Only accepts BNB coins")
		}
	}
	return NewMsgNoOp(signer), nil
}

func getMsgSwapFromMemo(memo SwapMemo, tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	if len(tx.Tx.Coins) > 1 {
		return nil, errors.New("not expecting multiple coins in a swap")
	}
	if memo.Destination.IsEmpty() {
		memo.Destination = tx.Tx.FromAddress
	}

	coin := tx.Tx.Coins[0]
	if memo.Asset.Equals(coin.Asset) {
		return nil, errors.Errorf("swap from %s to %s is noop, refund", memo.Asset.String(), coin.Asset.String())
	}

	// Looks like at the moment THORNode can only process ont ty
	return NewMsgSwap(tx.Tx, memo.GetAsset(), memo.Destination, memo.SlipLimit, signer), nil
}

func getMsgUnstakeFromMemo(memo WithdrawMemo, tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	withdrawAmount := sdk.NewUint(MaxWithdrawBasisPoints)
	if len(memo.GetAmount()) > 0 {
		withdrawAmount = sdk.NewUintFromString(memo.GetAmount())
	}
	return NewMsgSetUnStake(tx.Tx, tx.Tx.FromAddress, withdrawAmount, memo.GetAsset(), signer), nil
}

func getMsgStakeFromMemo(ctx sdk.Context, memo StakeMemo, tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	if len(tx.Tx.Coins) > 2 {
		return nil, errors.New("not expecting more than two coins in a stake")
	}
	runeAmount := sdk.ZeroUint()
	assetAmount := sdk.ZeroUint()
	asset := memo.GetAsset()
	if asset.IsEmpty() {
		return nil, errors.New("Unable to determine the intended pool for this stake")
	}
	if asset.IsRune() {
		return nil, errors.New("invalid pool asset")
	}
	for _, coin := range tx.Tx.Coins {
		ctx.Logger().Info("coin", "asset", coin.Asset.String(), "amount", coin.Amount.String())
		if coin.Asset.IsRune() {
			runeAmount = coin.Amount
		}
		if asset.Equals(coin.Asset) {
			assetAmount = coin.Amount
		}
	}

	if runeAmount.IsZero() && assetAmount.IsZero() {
		return nil, errors.New("did not find any valid coins for stake")
	}

	// when THORNode receive two coins, but THORNode didn't find the coin specify by asset, then user might send in the wrong coin
	if assetAmount.IsZero() && len(tx.Tx.Coins) == 2 {
		return nil, errors.Errorf("did not find %s ", asset)
	}

	runeAddr := tx.Tx.FromAddress
	assetAddr := memo.GetDestination()
	if !runeAddr.IsChain(common.BNBChain) {
		runeAddr = memo.GetDestination()
		assetAddr = tx.Tx.FromAddress
	} else {
		// if it is on BNB chain , while the asset addr is empty, then the asset addr is runeAddr
		if assetAddr.IsEmpty() {
			assetAddr = runeAddr
		}
	}

	return NewMsgSetStakeData(
		tx.Tx,
		asset,
		runeAmount,
		assetAmount,
		runeAddr,
		assetAddr,
		signer,
	), nil
}

func getMsgSetPoolDataFromMemo(ctx sdk.Context, keeper Keeper, memo CreateMemo, signer sdk.AccAddress) (sdk.Msg, error) {
	if keeper.PoolExist(ctx, memo.GetAsset()) {
		return nil, errors.New("pool already exists")
	}
	return NewMsgSetPoolData(
		memo.GetAsset(),
		PoolEnabled, // new pools start in a Bootstrap state
		signer,
	), nil
}

func getMsgAddFromMemo(memo AddMemo, tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	runeAmount := sdk.ZeroUint()
	assetAmount := sdk.ZeroUint()
	for _, coin := range tx.Tx.Coins {
		if coin.Asset.IsRune() {
			runeAmount = coin.Amount
		} else if memo.GetAsset().Equals(coin.Asset) {
			assetAmount = coin.Amount
		}
	}
	return NewMsgAdd(
		tx.Tx,
		memo.GetAsset(),
		runeAmount,
		assetAmount,
		signer,
	), nil
}

func getMsgOutboundFromMemo(memo OutboundMemo, tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	return NewMsgOutboundTx(
		tx,
		memo.GetTxID(),
		signer,
	), nil
}

func getMsgBondFromMemo(memo BondMemo, tx ObservedTx, signer sdk.AccAddress) (sdk.Msg, error) {
	runeAmount := sdk.ZeroUint()
	for _, coin := range tx.Tx.Coins {
		if coin.Asset.IsRune() {
			runeAmount = coin.Amount
		}
	}
	if runeAmount.IsZero() {
		return nil, errors.New("RUNE amount is 0")
	}
	return NewMsgBond(memo.GetNodeAddress(), runeAmount, tx.Tx.ID, tx.Tx.FromAddress, signer), nil
}

// handleMsgOutboundTx processes outbound tx from our pool
func handleMsgOutboundTx(ctx sdk.Context, keeper Keeper, poolAddressMgr PoolAddressManager, msg MsgOutboundTx) sdk.Result {
	ctx.Logger().Info(fmt.Sprintf("receive MsgOutboundTx %s", msg.Tx.Tx.ID))
	if !isSignedByActiveObserver(ctx, keeper, msg.GetSigners()) {
		ctx.Logger().Error("message signed by unauthorized account", "signer", msg.GetSigners())
		return sdk.ErrUnauthorized("Not authorized").Result()
	}
	if err := msg.ValidateBasic(); nil != err {
		ctx.Logger().Error("invalid MsgOutboundTx", "error", err)
		return err.Result()
	}
	currentChainPoolAddr := poolAddressMgr.GetCurrentPoolAddresses().Current.GetByChain(msg.Tx.Tx.Chain)
	if nil == currentChainPoolAddr {
		msg := fmt.Sprintf("THORNode don't have pool for chain %s", msg.Tx.Tx.Chain)
		ctx.Logger().Error(msg)
		return sdk.ErrUnknownRequest(msg).Result()
	}

	currentPoolAddr, err := currentChainPoolAddr.GetAddress()
	if nil != err {
		ctx.Logger().Error("fail to get current pool address", "error", err)
		return sdk.ErrUnknownRequest("fail to get current pool address").Result()
	}
	previousChainPoolAddr := poolAddressMgr.GetCurrentPoolAddresses().Previous.GetByChain(msg.Tx.Tx.Chain)
	previousPoolAddr := common.NoAddress
	if nil != previousChainPoolAddr {
		previousPoolAddr, err = previousChainPoolAddr.GetAddress()
		if nil != err {
			ctx.Logger().Error("fail to get previous pool address", "error", err)
			return sdk.ErrUnknownRequest("fail to get previous pool address").Result()
		}
	}

	if !currentPoolAddr.Equals(msg.Tx.Tx.FromAddress) && !previousPoolAddr.Equals(msg.Tx.Tx.FromAddress) {
		ctx.Logger().Error("message sent by unauthorized account", "sender", msg.Tx.Tx.FromAddress.String(), "current pool addr", currentPoolAddr.String())
		return sdk.ErrUnauthorized("Not authorized").Result()
	}

	voter, err := keeper.GetObservedTxVoter(ctx, msg.InTxID)
	if err != nil {
		ctx.Logger().Error(err.Error())
		return sdk.ErrInternal("fail to get observed tx voter").Result()
	}
	voter.AddOutTx(msg.Tx.Tx)
	keeper.SetObservedTxVoter(ctx, voter)

	// complete events
	if voter.IsDone() {
		err := completeEvents(ctx, keeper, msg.InTxID, voter.OutTxs)
		if err != nil {
			ctx.Logger().Error("unable to complete events", "error", err)
			return sdk.ErrInternal(err.Error()).Result()
		}
	}

	// Apply Gas fees
	if err := AddGasFees(ctx, keeper, msg.Tx); nil != err {
		ctx.Logger().Error("fail to add gas fee", err)
		return sdk.ErrInternal("fail to add gas fee").Result()
	}

	// update txOut record with our TxID that sent funds out of the pool
	txOut, err := keeper.GetTxOut(ctx, uint64(voter.Height))
	if err != nil {
		ctx.Logger().Error("unable to get txOut record", "error", err)
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}

	// Save TxOut back with the TxID only when the TxOut on the block height is
	// not empty
	if !txOut.IsEmpty() {
		for i, tx := range txOut.TxArray {

			// withdraw , refund etc, one inbound tx might result two outbound txes, THORNode have to correlate outbound tx back to the
			// inbound, and also txitem , thus THORNode could record both outbound tx hash correctly
			// given every tx item will only have one coin in it , given that , THORNode could use that to identify which txit
			if tx.InHash.Equals(msg.InTxID) &&
				tx.OutHash.IsEmpty() &&
				msg.Tx.Tx.Coins.Contains(tx.Coin) {
				txOut.TxArray[i].OutHash = msg.Tx.Tx.ID
			}
		}
		if err := keeper.SetTxOut(ctx, txOut); nil != err {
			ctx.Logger().Error("fail to save tx out", err)
			return sdk.ErrInternal("fail to save tx out").Result()
		}
	}
	keeper.SetLastSignedHeight(ctx, sdk.NewUint(uint64(voter.Height)))

	// If THORNode are sending from a yggdrasil pool, decrement coins on record
	if keeper.YggdrasilExists(ctx, msg.Tx.ObservedPubKey) {
		ygg, err := keeper.GetYggdrasil(ctx, msg.Tx.ObservedPubKey)
		if nil != err {
			ctx.Logger().Error("fail to get yggdrasil", err)
			return sdk.ErrInternal("fail to get yggdrasil").Result()
		}
		ygg.SubFunds(msg.Tx.Tx.Coins)
		if err := keeper.SetYggdrasil(ctx, ygg); nil != err {
			ctx.Logger().Error("fail to save yggdrasil", err)
			return sdk.ErrInternal("fail to save yggdrasil").Result()
		}
	}

	return sdk.Result{
		Code:      sdk.CodeOK,
		Codespace: DefaultCodespace,
	}
}

// handleMsgSetAdminConfig process admin config
func handleMsgSetAdminConfig(ctx sdk.Context, keeper Keeper, msg MsgSetAdminConfig) sdk.Result {
	ctx.Logger().Info(fmt.Sprintf("receive MsgSetAdminConfig %s --> %s", msg.AdminConfig.Key, msg.AdminConfig.Value))
	if !isSignedByActiveNodeAccounts(ctx, keeper, msg.GetSigners()) {
		ctx.Logger().Error("message signed by unauthorized account")
		return sdk.ErrUnauthorized("Not authorized").Result()
	}
	if err := msg.ValidateBasic(); nil != err {
		ctx.Logger().Error("invalid MsgSetAdminConfig", "error", err)
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}

	prevVal, err := keeper.GetAdminConfigValue(ctx, msg.AdminConfig.Key, nil)
	if err != nil {
		ctx.Logger().Error("unable to get admin config", "error", err)
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}

	keeper.SetAdminConfig(ctx, msg.AdminConfig)

	newVal, err := keeper.GetAdminConfigValue(ctx, msg.AdminConfig.Key, nil)
	if err != nil {
		ctx.Logger().Error("unable to get admin config", "error", err)
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}

	if newVal != "" && prevVal != newVal {
		adminEvt := NewEventAdminConfig(
			msg.AdminConfig.Key.String(),
			msg.AdminConfig.Value,
		)
		stakeBytes, err := json.Marshal(adminEvt)
		if err != nil {
			ctx.Logger().Error("fail to unmarshal admin config event", err)
			err = errors.Wrap(err, "fail to marshal admin config event to json")
			return sdk.ErrUnknownRequest(err.Error()).Result()
		}

		evt := NewEvent(
			adminEvt.Type(),
			ctx.BlockHeight(),
			msg.Tx,
			stakeBytes,
			EventSuccess,
		)
		keeper.SetCompletedEvent(ctx, evt)
	}

	return sdk.Result{
		Code:      sdk.CodeOK,
		Codespace: DefaultCodespace,
	}
}

// handleMsgSetTrustAccount Update node account
func handleMsgSetTrustAccount(ctx sdk.Context, keeper Keeper, msg MsgSetTrustAccount) sdk.Result {
	ctx.Logger().Info("receive MsgSetTrustAccount", "validator consensus pub key", msg.ValidatorConsPubKey, "pubkey", msg.NodePubKeys.String())
	nodeAccount, err := keeper.GetNodeAccount(ctx, msg.Signer)
	if err != nil {
		ctx.Logger().Error("fail to get node account", "error", err, "address", msg.Signer.String())
		return sdk.ErrUnauthorized(fmt.Sprintf("%s is not authorizaed", msg.Signer)).Result()
	}
	if nodeAccount.IsEmpty() {
		ctx.Logger().Error("unauthorized account", "address", msg.Signer.String())
		return sdk.ErrUnauthorized(fmt.Sprintf("%s is not authorizaed", msg.Signer)).Result()
	}
	if err := msg.ValidateBasic(); err != nil {
		ctx.Logger().Error("MsgUpdateNodeAccount is invalid", "error", err)
		return sdk.ErrUnknownRequest("MsgUpdateNodeAccount is invalid").Result()
	}

	// You should not able to update node address when the node is in active mode
	// for example if they update observer address
	if nodeAccount.Status == NodeActive {
		ctx.Logger().Error(fmt.Sprintf("node %s is active, so it can't update itself", nodeAccount.NodeAddress))
		return sdk.ErrUnknownRequest("node is active can't update").Result()
	}
	if nodeAccount.Status == NodeDisabled {
		ctx.Logger().Error(fmt.Sprintf("node %s is disabled, so it can't update itself", nodeAccount.NodeAddress))
		return sdk.ErrUnknownRequest("node is disabled can't update").Result()
	}
	if err := keeper.EnsureTrustAccountUnique(ctx, msg.ValidatorConsPubKey, msg.NodePubKeys); nil != err {
		ctx.Logger().Error("Unable to ensure trust account uniqueness", "error", err)
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}
	// Here make sure THORNode don't change the node account's bond

	nodeAccount.Status = NodeReady // TODO: should check version is set, and observer is ready monitoring chains
	nodeAccount.ValidatorConsPubKey = msg.ValidatorConsPubKey
	nodeAccount.NodePubKey = msg.NodePubKeys
	nodeAccount.UpdateStatus(NodeStandby, ctx.BlockHeight())
	if err := keeper.SetNodeAccount(ctx, nodeAccount); nil != err {
		ctx.Logger().Error(fmt.Sprintf("fail to save node account: %s", nodeAccount), err)
		return sdk.ErrInternal("fail to save node account").Result()
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent("set_trust_account",
			sdk.NewAttribute("node_address", msg.Signer.String()),
			sdk.NewAttribute("node_secp256k1_pubkey", msg.NodePubKeys.Secp256k1.String()),
			sdk.NewAttribute("node_ed25519_pubkey", msg.NodePubKeys.Ed25519.String()),
			sdk.NewAttribute("validator_consensus_pub_key", msg.ValidatorConsPubKey)))
	ctx.Logger().Info("completed MsgSetTrustAccount")
	return sdk.Result{
		Code:      sdk.CodeOK,
		Codespace: DefaultCodespace,
	}
}
