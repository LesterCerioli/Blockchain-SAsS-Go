{-# LANGUAGE DataKinds #-}
{-# LANGUAGE DeriveAnyClass #-}
{-# LANGUAGE DeriveGeneric #-}
{-# LANGUAGE NoImplicitPrelude #-}
{-# LANGUAGE OverloadedStrings #-}
{-# LANGUAGE ScopedTypeVariables #-}
{-# LANGUAGE TemplateHaskell #-}
{-# LANGUAGE TypeApplications #-}

-- | Time-locked escrow validator for the Cardano adapter.
--
-- Reference implementation only. It must be compiled with the matching Plutus
-- toolchain and audited before use on mainnet. The datum captures the merchant,
-- beneficiary and release time; the redeemer selects release or refund.
module Escrow where

import           PlutusTx
import           PlutusTx.Prelude
import           Plutus.V2.Ledger.Api
import qualified Plutus.V2.Ledger.Contexts as V2

data EscrowDatum = EscrowDatum
    { merchant    :: PubKeyHash
    , beneficiary :: PubKeyHash
    , arbiter     :: PubKeyHash
    , releaseAt   :: POSIXTime
    }
PlutusTx.unstableMakeIsData ''EscrowDatum

data EscrowRedeemer
    = Release
    | Refund
    | Resolve Bool
PlutusTx.unstableMakeIsData ''EscrowRedeemer

{-# INLINABLE escrowValidator #-}
escrowValidator :: EscrowDatum -> EscrowRedeemer -> ScriptContext -> Bool
escrowValidator datum redeemer ctx =
    case redeemer of
        Release ->
            traceIfFalse "release time not reached" (contains (from (releaseAt datum)) (V2.txInfoValidRange info))
                && traceIfFalse "beneficiary did not sign" (V2.txSignedBy info (beneficiary datum))
        Refund ->
            traceIfFalse "merchant did not sign" (V2.txSignedBy info (merchant datum))
        Resolve _ ->
            traceIfFalse "arbiter did not sign" (V2.txSignedBy info (arbiter datum))
  where
    info = V2.scriptContextTxInfo ctx

-- | The validator must be applied to the script address that holds the escrow
-- value; the gateway stores the datum alongside the payment intent.
escrowScript :: Script
escrowScript = fromCompiledCode $$(PlutusTx.compile [|| escrowValidator ||])

escrowValidatorHash :: ValidatorHash
escrowValidatorHash = validatorHash $ mkValidatorScript ($$(PlutusTx.compile [|| escrowValidator ||]))

escrowAddress :: Address
escrowAddress = scriptHashAddress escrowValidatorHash
