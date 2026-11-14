# Smart Contracts

Reference on-chain settlement contracts for the Blockchain Payment Gateway. They
mirror the escrow state machine implemented in `internal/core/domain/entities/escrow.go`:
`pending -> funded -> released | refunded | canceled | expired`, with dispute
resolution for arbiters.

| Chain | Path | Toolchain |
| --- | --- | --- |
| Ethereum | `ethereum/PaymentEscrow.sol` | Solidity `^0.8.24`, Foundry or Hardhat |
| Solana | `solana/programs/payment-escrow` | Anchor `0.30.x`, Solana CLI |
| Cardano | `cardano/Escrow.hs` | Plutus V2, `plutus-tx` + `plutus-apps` |

## Ethereum

```bash
forge build contracts/ethereum/PaymentEscrow.sol
# or
npx hardhat compile
```

The contract is native-currency only. ERC-20/SPL/ADA token support is handled by
the gateway adapters and can be added by swapping the transfer calls for token
program invocations.

## Solana

```bash
cd contracts/solana
anchor build
anchor test
```

`initialize_escrow` creates a PDA seeded by `["escrow", payer, reference]` and
funds it. `release` requires the release time to have passed, `refund` returns
the lamports to the payer, and `dispute`/`resolve` cover arbiter flows.

## Cardano

Compile with the Plutus toolchain and deploy the script address produced by
`escrowAddress`. The datum stores merchant, beneficiary, arbiter and release
time; the redeemer selects `Release`, `Refund` or `Resolve`.

## Security

These contracts are reference implementations for the gateway integration. They
have not been audited. Before mainnet use:

- add property/invariant tests and fuzzing,
- review access control and reentrancy assumptions,
- verify time-lock semantics against chain clock drift,
- run an independent security audit.
