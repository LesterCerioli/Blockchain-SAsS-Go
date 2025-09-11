package types

type BlockchainType string

const (
	BlockchainBitcoin           BlockchainType = "bitcoin"
	BlockchainEthereum          BlockchainType = "ethereum"
	BlockchainSolana            BlockchainType = "solana"
	BlockchainBinanceSmartChain BlockchainType = "bsc"
	BlockchainPolygon           BlockchainType = "polygon"
	BlockchainCardano           BlockchainType = "cardano"
	BlockchainAvalanche         BlockchainType = "avalanche"
	BlockchainPolkadot          BlockchainType = "polkadot"
	BlockchainCosmos            BlockchainType = "cosmos"
)

type BlockchainNetwork string

const (
	NetworkMainnet BlockchainNetwork = "mainnet"
	NetworkTestnet BlockchainNetwork = "testnet"
	NetworkDevnet  BlockchainNetwork = "devnet"
	NetworkRegtest BlockchainNetwork = "regtest"

	NetworkBitcoinMainnet BlockchainNetwork = "bitcoin_mainnet"
	NetworkBitcoinTestnet BlockchainNetwork = "bitcoin_testnet"
	NetworkBitcoinRegtest BlockchainNetwork = "bitcoin_regtest"
	NetworkBitcoinSignet  BlockchainNetwork = "bitcoin_signet"

	NetworkEthereumMainnet BlockchainNetwork = "ethereum_mainnet"
	NetworkEthereumGoerli  BlockchainNetwork = "ethereum_goerli"
	NetworkEthereumSepolia BlockchainNetwork = "ethereum_sepolia"
	NetworkArbitrum        BlockchainNetwork = "arbitrum"
	NetworkOptimism        BlockchainNetwork = "optimism"
)

type BlockchainInfo struct {
	Type                     BlockchainType    `json:"type"`
	Network                  BlockchainNetwork `json:"network"`
	Name                     string            `json:"name"`
	Symbol                   string            `json:"symbol"`
	Decimals                 int               `json:"decimals"`
	BlockTime                float64           `json:"block_time"`
	RequiredConfirmations    int               `json:"required_confirmations"`
	IsTokenSupported         bool              `json:"is_token_supported"`
	IsSmartContractSupported bool              `json:"is_smart_contract_supported"`
	IsStakingSupported       bool              `json:"is_staking_supported"`
	Website                  string            `json:"website,omitempty"`
	ExplorerURL              string            `json:"explorer_url,omitempty"`
	RPCURL                   string            `json:"rpc_url,omitempty"`
}

type ChainID int64

const (
	ChainIDBitcoinMainnet  ChainID = 0
	ChainIDBitcoinTestnet  ChainID = 1
	ChainIDEthereumMainnet ChainID = 1
	ChainIDEthereumGoerli  ChainID = 5
	ChainIDEthereumSepolia ChainID = 11155111
	ChainIDBSCMainnet      ChainID = 56
	ChainIDBSCTestnet      ChainID = 97
	ChainIDPolygonMainnet  ChainID = 137
	ChainIDSolanaMainnet   ChainID = 101
	ChainIDSolanaTestnet   ChainID = 102
	ChainIDSolanaDevnet    ChainID = 103
)

func (n BlockchainNetwork) IsTestnet() bool {
	return n == NetworkTestnet || n == NetworkDevnet || n == NetworkRegtest ||
		n == NetworkBitcoinTestnet || n == NetworkBitcoinRegtest || n == NetworkBitcoinSignet ||
		n == NetworkEthereumGoerli || n == NetworkEthereumSepolia
}

func (n BlockchainNetwork) IsMainnet() bool {
	return n == NetworkMainnet || n == NetworkBitcoinMainnet || n == NetworkEthereumMainnet
}

func GetBlockchainInfo(blockchain BlockchainType, network BlockchainNetwork) BlockchainInfo {
	switch blockchain {
	case BlockchainBitcoin:
		info := BlockchainInfo{
			Type:                     BlockchainBitcoin,
			Network:                  network,
			Name:                     "Bitcoin",
			Symbol:                   "BTC",
			Decimals:                 8,
			BlockTime:                10.0,
			RequiredConfirmations:    6,
			IsTokenSupported:         false,
			IsSmartContractSupported: false,
			IsStakingSupported:       false,
			Website:                  "https://bitcoin.org",
			ExplorerURL:              "https://blockstream.info",
		}

		switch network {
		case NetworkBitcoinMainnet:
			info.Name = "Bitcoin Mainnet"
		case NetworkBitcoinTestnet:
			info.Name = "Bitcoin Testnet"
			info.ExplorerURL = "https://blockstream.info/testnet"
		case NetworkBitcoinRegtest:
			info.Name = "Bitcoin Regtest"
			info.BlockTime = 1.0
		case NetworkBitcoinSignet:
			info.Name = "Bitcoin Signet"
			info.ExplorerURL = "https://explorer.bc-2.jp"
		}

		return info

	case BlockchainEthereum:
		info := BlockchainInfo{
			Type:                     BlockchainEthereum,
			Network:                  network,
			Name:                     "Ethereum",
			Symbol:                   "ETH",
			Decimals:                 18,
			BlockTime:                12.0,
			RequiredConfirmations:    12,
			IsTokenSupported:         true,
			IsSmartContractSupported: true,
			IsStakingSupported:       true,
			Website:                  "https://ethereum.org",
			ExplorerURL:              "https://etherscan.io",
		}

		switch network {
		case NetworkEthereumMainnet:
			info.Name = "Ethereum Mainnet"
		case NetworkEthereumGoerli:
			info.Name = "Ethereum Goerli"
			info.ExplorerURL = "https://goerli.etherscan.io"
		case NetworkEthereumSepolia:
			info.Name = "Ethereum Sepolia"
			info.ExplorerURL = "https://sepolia.etherscan.io"
		case NetworkArbitrum:
			info.Name = "Arbitrum"
			info.Symbol = "ETH"
			info.Website = "https://arbitrum.io"
			info.ExplorerURL = "https://arbiscan.io"
		case NetworkOptimism:
			info.Name = "Optimism"
			info.Symbol = "ETH"
			info.Website = "https://optimism.io"
			info.ExplorerURL = "https://optimistic.etherscan.io"
		}

		return info

	default:
		return BlockchainInfo{
			Type:                     blockchain,
			Network:                  network,
			Name:                     string(blockchain),
			Symbol:                   string(blockchain),
			Decimals:                 18,
			BlockTime:                5.0,
			RequiredConfirmations:    6,
			IsTokenSupported:         false,
			IsSmartContractSupported: false,
			IsStakingSupported:       false,
		}
	}
}

func (b BlockchainType) ValidateAddress(address string, network BlockchainNetwork) bool {

	switch b {
	case BlockchainBitcoin:
		return ValidateBitcoinAddress(address, network)
	case BlockchainEthereum:
		return len(address) == 42 && address[:2] == "0x"
	case BlockchainSolana:
		return len(address) >= 32 && len(address) <= 44
	default:
		return len(address) > 0
	}
}

func (b BlockchainType) String() string {
	return string(b)
}
