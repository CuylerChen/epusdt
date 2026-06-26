package task

import (
	"context"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/model/service"
	"github.com/GMWalletApp/epusdt/util/log"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type ethRecipientSnapshot struct {
	addrs map[string]struct{}
}

var ethWatchedRecipients atomic.Pointer[ethRecipientSnapshot]

func StartEthereumWebSocketListener() {
	// Wait until the chain is enabled AND at least one token contract
	// is configured. Polls every 10s so admin-side toggles kick in
	// without a restart. Once conditions are met we proceed to connect;
	// if the websocket later drops we exit the loop and rely on the
	// process-level restart to reconnect (same as before this refactor).
	for {
		if data.IsChainEnabled(mdb.NetworkEthereum) {
			if contracts := loadChainTokenContracts(mdb.NetworkEthereum, "[ETH-WS]"); len(contracts) > 0 {
				runEthereumListener(contracts)
			}
		}
		time.Sleep(10 * time.Second)
	}
}

func runEthereumListener(contracts []common.Address) {
	wallets, err := data.GetAvailableWalletAddressByNetwork(mdb.NetworkEthereum)
	if err != nil {
		log.Sugar.Errorf("[ETH-WS] Failed to get wallet addresses: %v", err)
		return
	}
	recipientTopics := evmRecipientTopicsFromWallets(wallets)
	if len(recipientTopics) == 0 {
		log.Sugar.Warnf("[ETH-WS] no enabled recipient wallet addresses, skip websocket subscription")
		return
	}
	StoreEthRecipientsFromWallets(wallets)

	ctx, cancel := chainEnabledWatchdog(mdb.NetworkEthereum, "[ETH-WS]", chainTokenFingerprint(mdb.NetworkEthereum))
	defer cancel()
	watchEvmRecipientChanges(ctx, cancel, mdb.NetworkEthereum, "[ETH-WS]", evmRecipientFingerprintFromWallets(wallets))

	wsNode, ok := resolveChainWsNode(mdb.NetworkEthereum, "[ETH-WS]")
	if !ok {
		return
	}
	log.Sugar.Infof("[ETH-WS] connecting using WSS node %s watching %d contract(s), %d recipient(s)", data.RpcNodeLogLabel(wsNode), len(contracts), len(recipientTopics))

	query := evmTransferFilterQuery(contracts, recipientTopics)

	runEvmWsLogListener(ctx, "[ETH-WS]", wsNode, query, func(client *ethclient.Client, vLog types.Log) {
		if len(vLog.Topics) < 3 {
			return
		}
		event := vLog.Topics[0].String()
		if event != transferEventHash.String() {
			return
		}

		amount := new(big.Int).SetBytes(vLog.Data)
		toAddr := common.HexToAddress(vLog.Topics[2].Hex())
		if !isWatchedEthRecipient(toAddr) {
			return
		}

		var blockTsMs int64
		header, err := client.HeaderByNumber(context.Background(), big.NewInt(int64(vLog.BlockNumber)))
		if err != nil {
			log.Sugar.Warnf("[ETH-WS] HeaderByNumber block=%d: %v, using local time", vLog.BlockNumber, err)
			blockTsMs = time.Now().UnixMilli()
		} else {
			blockTsMs = int64(header.Time) * 1000
		}

		service.TryProcessEvmERC20Transfer(mdb.NetworkEthereum, vLog.Address, toAddr, amount, vLog.TxHash.Hex(), blockTsMs)
	})
}

func StoreEthRecipientsFromWallets(wallets []mdb.WalletAddress) int {
	m := make(map[string]struct{})
	for _, w := range wallets {
		a := strings.TrimSpace(w.Address)
		if !common.IsHexAddress(a) {
			continue
		}
		m[strings.ToLower(common.HexToAddress(a).Hex())] = struct{}{}
	}
	ethWatchedRecipients.Store(&ethRecipientSnapshot{addrs: m})
	return len(m)
}

func isWatchedEthRecipient(to common.Address) bool {
	snap := ethWatchedRecipients.Load()
	if snap == nil || len(snap.addrs) == 0 {
		return false
	}
	_, ok := snap.addrs[strings.ToLower(to.Hex())]
	return ok
}
