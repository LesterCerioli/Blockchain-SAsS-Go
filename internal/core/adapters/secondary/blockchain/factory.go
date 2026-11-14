package blockchain

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type AdapterOptions struct {
	Enabled               bool
	RPCURL                string
	RPCUser               string
	RPCPassword           string
	APIKey                string
	DepositAddress        string
	Network               types.BlockchainNetwork
	RequiredConfirmations int
	LookbackBlocks        int
	Timeout               time.Duration
	FeeMultiplier         float64
}

type Options struct {
	Chains map[types.BlockchainType]AdapterOptions
	Mock   bool
}

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]ports.BlockchainPort
}

func NewRegistry(options Options) *Registry {
	registry := &Registry{adapters: make(map[string]ports.BlockchainPort)}

	for chain, chainOptions := range options.Chains {
		if !chainOptions.Enabled {
			continue
		}
		network := chainOptions.Network
		var adapter ports.BlockchainPort
		if options.Mock {
			adapter = NewMockAdapter(chain, network, chainOptions.RequiredConfirmations)
		} else {
			switch chain {
			case types.BlockchainEthereum:
				adapter = NewEthereumAdapter(EthereumOptions{
					RPCURL: chainOptions.RPCURL, DepositAddress: chainOptions.DepositAddress,
					Network: network, RequiredConfirmations: chainOptions.RequiredConfirmations,
					LookbackBlocks: chainOptions.LookbackBlocks, Timeout: chainOptions.Timeout,
					FeeMultiplier: chainOptions.FeeMultiplier,
				})
			case types.BlockchainBitcoin:
				adapter = NewBitcoinAdapter(BitcoinOptions{
					RPCURL: chainOptions.RPCURL, RPCUser: chainOptions.RPCUser,
					RPCPassword: chainOptions.RPCPassword, DepositAddress: chainOptions.DepositAddress,
					Network: network, RequiredConfirmations: chainOptions.RequiredConfirmations,
					Timeout: chainOptions.Timeout, FeeMultiplier: chainOptions.FeeMultiplier,
				})
			case types.BlockchainSolana:
				adapter = NewSolanaAdapter(SolanaOptions{
					RPCURL: chainOptions.RPCURL, DepositAddress: chainOptions.DepositAddress,
					Network: network, RequiredConfirmations: chainOptions.RequiredConfirmations,
					Timeout: chainOptions.Timeout, FeeMultiplier: chainOptions.FeeMultiplier,
				})
			case types.BlockchainCardano:
				adapter = NewCardanoAdapter(CardanoOptions{
					APIURL: chainOptions.RPCURL, APIKey: chainOptions.APIKey,
					DepositAddress: chainOptions.DepositAddress, Network: network,
					RequiredConfirmations: chainOptions.RequiredConfirmations,
					Timeout:               chainOptions.Timeout, FeeMultiplier: chainOptions.FeeMultiplier,
				})
			}
		}
		if adapter != nil {
			registry.adapters[registryKey(chain, network)] = adapter
		}
	}
	return registry
}

func registryKey(chain types.BlockchainType, network types.BlockchainNetwork) string {
	return fmt.Sprintf("%s|%s", chain, network)
}

func (r *Registry) Get(chain types.BlockchainType, network types.BlockchainNetwork) (ports.BlockchainPort, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if adapter, ok := r.adapters[registryKey(chain, network)]; ok {
		return adapter, nil
	}
	for key, adapter := range r.adapters {
		if adapter.Type() == chain {
			return adapter, nil
		}
		_ = key
	}
	return nil, fmt.Errorf("%w: no adapter for %s/%s", entities.ErrUnsupported, chain, network)
}

func (r *Registry) Supported() []types.BlockchainInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]types.BlockchainInfo, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		infos = append(infos, adapter.Info())
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Type == infos[j].Type {
			return infos[i].Network < infos[j].Network
		}
		return infos[i].Type < infos[j].Type
	})
	return infos
}

func (r *Registry) Health(ctx context.Context) map[string]error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]error, len(r.adapters))
	for _, adapter := range r.adapters {
		result[adapter.Name()+"/"+string(adapter.Network())] = adapter.Health(ctx)
	}
	return result
}

var _ ports.BlockchainRegistry = (*Registry)(nil)
