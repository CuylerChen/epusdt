package task

import (
	"testing"

	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"

	"github.com/ethereum/go-ethereum/common"
)

func TestResolveChainWsURLRequiresEnabledRpcNode(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	if got, ok := resolveChainWsURL(mdb.NetworkEthereum, "[TEST]"); ok {
		t.Fatalf("resolveChainWsURL() = (%q, true), want false", got)
	}
}

func TestResolveChainWsURLWithRow(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	node := &mdb.RpcNode{
		Network: mdb.NetworkEthereum,
		Url:     " wss://ethereum.example.com ",
		Type:    mdb.RpcNodeTypeWs,
		Weight:  1,
		Enabled: true,
		Status:  mdb.RpcNodeStatusOk,
	}
	if err := dao.Mdb.Create(node).Error; err != nil {
		t.Fatalf("seed rpc_node: %v", err)
	}

	got, ok := resolveChainWsURL(mdb.NetworkEthereum, "[TEST]")
	if !ok {
		t.Fatalf("resolveChainWsURL() ok=false, want true")
	}
	if got != "wss://ethereum.example.com" {
		t.Fatalf("resolveChainWsURL() = %q, want wss://ethereum.example.com", got)
	}
}

func TestResolveChainWsURLIgnoresManualVerifyOnly(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	if err := dao.Mdb.Create(&mdb.RpcNode{
		Network: mdb.NetworkEthereum,
		Url:     "wss://paid-ethereum.example.com",
		Type:    mdb.RpcNodeTypeWs,
		Weight:  100,
		Enabled: true,
		Purpose: mdb.RpcNodePurposeManualVerify,
		Status:  mdb.RpcNodeStatusOk,
	}).Error; err != nil {
		t.Fatalf("seed manual rpc_node: %v", err)
	}

	if got, ok := resolveChainWsURL(mdb.NetworkEthereum, "[TEST]"); ok {
		t.Fatalf("resolveChainWsURL() = (%q, true), want false", got)
	}
}

func TestResolveChainWsURLUsesGeneralWhenManualVerifyExists(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	rows := []mdb.RpcNode{
		{Network: mdb.NetworkEthereum, Url: "wss://paid-ethereum.example.com", Type: mdb.RpcNodeTypeWs, Weight: 100, Enabled: true, Purpose: mdb.RpcNodePurposeManualVerify, Status: mdb.RpcNodeStatusOk},
		{Network: mdb.NetworkEthereum, Url: " wss://general-ethereum.example.com ", Type: mdb.RpcNodeTypeWs, Weight: 1, Enabled: true, Purpose: mdb.RpcNodePurposeGeneral, Status: mdb.RpcNodeStatusOk},
	}
	if err := dao.Mdb.Create(&rows).Error; err != nil {
		t.Fatalf("seed rpc_nodes: %v", err)
	}

	got, ok := resolveChainWsURL(mdb.NetworkEthereum, "[TEST]")
	if !ok {
		t.Fatalf("resolveChainWsURL() ok=false, want true")
	}
	if got != "wss://general-ethereum.example.com" {
		t.Fatalf("resolveChainWsURL() = %q, want general node", got)
	}
}

func TestResolveChainWsNodeSkipsCoolingNode(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	data.ResetRpcFailoverForTest()
	t.Cleanup(data.ResetRpcFailoverForTest)

	rows := []mdb.RpcNode{
		{Network: mdb.NetworkEthereum, Url: "wss://primary-ethereum.example.com", Type: mdb.RpcNodeTypeWs, Weight: 100, Enabled: true, Purpose: mdb.RpcNodePurposeGeneral, Status: mdb.RpcNodeStatusOk},
		{Network: mdb.NetworkEthereum, Url: "wss://backup-ethereum.example.com", Type: mdb.RpcNodeTypeWs, Weight: 1, Enabled: true, Purpose: mdb.RpcNodePurposeGeneral, Status: mdb.RpcNodeStatusOk},
	}
	if err := dao.Mdb.Create(&rows).Error; err != nil {
		t.Fatalf("seed rpc_nodes: %v", err)
	}

	primary, ok := resolveChainWsNode(mdb.NetworkEthereum, "[TEST]")
	if !ok || primary.Url != "wss://primary-ethereum.example.com" {
		t.Fatalf("primary node = %#v ok=%v, want primary", primary, ok)
	}
	for i := 0; i < data.RpcFailoverThreshold; i++ {
		data.RecordRpcNodeFailure(primary.ID)
	}

	got, ok := resolveChainWsNode(mdb.NetworkEthereum, "[TEST]")
	if !ok {
		t.Fatalf("resolveChainWsNode() ok=false, want true")
	}
	if got.Url != "wss://backup-ethereum.example.com" {
		t.Fatalf("resolveChainWsNode() = %#v, want backup", got)
	}
}

func TestResolveChainWsURLDisabledRow(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	node := &mdb.RpcNode{
		Network: mdb.NetworkEthereum,
		Url:     "wss://disabled.example.com",
		Type:    mdb.RpcNodeTypeWs,
		Weight:  1,
		Enabled: true,
		Status:  mdb.RpcNodeStatusOk,
	}
	if err := dao.Mdb.Create(node).Error; err != nil {
		t.Fatalf("seed rpc_node: %v", err)
	}
	if err := dao.Mdb.Model(node).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable rpc_node: %v", err)
	}

	if got, ok := resolveChainWsURL(mdb.NetworkEthereum, "[TEST]"); ok {
		t.Fatalf("resolveChainWsURL() = (%q, true), want false", got)
	}
}

func TestEvmRecipientTopicsFromWalletsPadsAddressesAsTopic2(t *testing.T) {
	wallets := []mdb.WalletAddress{
		{Address: "0x2222222222222222222222222222222222222222"},
		{Address: "0x1111111111111111111111111111111111111111"},
		{Address: "0x1111111111111111111111111111111111111111"},
		{Address: "not-an-address"},
	}

	got := evmRecipientTopicsFromWallets(wallets)

	want := []common.Hash{
		common.HexToHash("0x0000000000000000000000001111111111111111111111111111111111111111"),
		common.HexToHash("0x0000000000000000000000002222222222222222222222222222222222222222"),
	}
	if len(got) != len(want) {
		t.Fatalf("evmRecipientTopicsFromWallets() len=%d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("topic[%d]=%s, want %s", i, got[i], want[i])
		}
	}
}

func TestEvmTransferFilterQueryRestrictsTransferAndRecipientTopic(t *testing.T) {
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	recipientTopic := common.HexToHash("0x0000000000000000000000004444444444444444444444444444444444444444")

	got := evmTransferFilterQuery([]common.Address{contract}, []common.Hash{recipientTopic})

	if len(got.Addresses) != 1 || got.Addresses[0] != contract {
		t.Fatalf("Addresses=%#v, want only %s", got.Addresses, contract)
	}
	if len(got.Topics) != 3 {
		t.Fatalf("Topics len=%d, want 3", len(got.Topics))
	}
	if len(got.Topics[0]) != 1 || got.Topics[0][0] != transferEventHash {
		t.Fatalf("Topics[0]=%#v, want Transfer topic", got.Topics[0])
	}
	if got.Topics[1] != nil {
		t.Fatalf("Topics[1]=%#v, want nil wildcard", got.Topics[1])
	}
	if len(got.Topics[2]) != 1 || got.Topics[2][0] != recipientTopic {
		t.Fatalf("Topics[2]=%#v, want recipient topic", got.Topics[2])
	}
}
