package types

import "math/big"

// EthereumTxType representa o tipo de transação Ethereum (EIP-2718)
type EthereumTxType uint8

const (
	EthereumTxTypeLegacy  EthereumTxType = 0
	EthereumTxTypeEIP2930 EthereumTxType = 1
	EthereumTxTypeEIP1559 EthereumTxType = 2
)

// TokenType representa o tipo de token Ethereum
type TokenType string

const (
	TokenTypeERC20   TokenType = "ERC20"
	TokenTypeERC721  TokenType = "ERC721"
	TokenTypeERC1155 TokenType = "ERC1155"
)

// EthereumNetwork representa redes Ethereum específicas
type EthereumNetwork string

const (
	EthereumNetworkGoerli   EthereumNetwork = "goerli"
	EthereumNetworkSepolia  EthereumNetwork = "sepolia"
	EthereumNetworkArbitrum EthereumNetwork = "arbitrum"
	EthereumNetworkOptimism EthereumNetwork = "optimism"
)

// GasInfo contém informações sobre gas Ethereum
type GasInfo struct {
	Limit    uint64   `json:"limit"`
	Used     uint64   `json:"used"`
	Price    *big.Int `json:"price"`
	Priority *big.Int `json:"priority_fee,omitempty"`
	MaxFee   *big.Int `json:"max_fee,omitempty"`
}

// EthereumBlock representa um bloco Ethereum
type EthereumBlock struct {
	Number       *big.Int `json:"number"`
	Hash         string   `json:"hash"`
	ParentHash   string   `json:"parent_hash"`
	Timestamp    uint64   `json:"timestamp"`
	Transactions []string `json:"transactions"`
	GasUsed      uint64   `json:"gas_used"`
	GasLimit     uint64   `json:"gas_limit"`
	BaseFee      *big.Int `json:"base_fee,omitempty"`
}

// String retorna a representação string do tipo de transação Ethereum
func (t EthereumTxType) String() string {
	switch t {
	case EthereumTxTypeLegacy:
		return "legacy"
	case EthereumTxTypeEIP2930:
		return "eip2930"
	case EthereumTxTypeEIP1559:
		return "eip1559"
	default:
		return "unknown"
	}
}

// IsEIP1559 verifica se é uma transação EIP-1559
func (t EthereumTxType) IsEIP1559() bool {
	return t == EthereumTxTypeEIP1559
}

// IsLegacy verifica se é uma transação legada
func (t EthereumTxType) IsLegacy() bool {
	return t == EthereumTxTypeLegacy
}

// String retorna a representação string da rede Ethereum
func (n EthereumNetwork) String() string {
	return string(n)
}

// ToBlockchainNetwork converte EthereumNetwork para BlockchainNetwork
func (n EthereumNetwork) ToBlockchainNetwork() BlockchainNetwork {
	switch n {
	case EthereumNetworkGoerli:
		return NetworkEthereumGoerli
	case EthereumNetworkSepolia:
		return NetworkEthereumSepolia
	case EthereumNetworkArbitrum:
		return NetworkArbitrum
	case EthereumNetworkOptimism:
		return NetworkOptimism
	default:
		return BlockchainNetwork(n)
	}
}

// EthereumNetworkFromBlockchain converte BlockchainNetwork para EthereumNetwork
func EthereumNetworkFromBlockchain(network BlockchainNetwork) EthereumNetwork {
	switch network {
	case NetworkEthereumGoerli:
		return EthereumNetworkGoerli
	case NetworkEthereumSepolia:
		return EthereumNetworkSepolia
	case NetworkArbitrum:
		return EthereumNetworkArbitrum
	case NetworkOptimism:
		return EthereumNetworkOptimism
	default:
		return EthereumNetwork(network)
	}
}
