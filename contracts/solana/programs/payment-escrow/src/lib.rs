//! Time-locked escrow program for the Blockchain Payment Gateway.
//!
//! The gateway creates one escrow PDA per payment intent. Lamports are held by
//! the PDA and can only move to the merchant/beneficiary or back to the payer
//! according to the rules below. This is a reference implementation and MUST be
//! audited before handling real funds.

use anchor_lang::prelude::*;
use anchor_lang::system_program;

declare_id!("Fg6PaFpoGXkYsidMpWTK6W2BeZ7FEfcYkg476zPFsLnS");

#[program]
pub mod payment_escrow {
    use super::*;

    pub fn initialize_escrow(
        ctx: Context<InitializeEscrow>,
        amount: u64,
        release_at: i64,
        reference: String,
    ) -> Result<()> {
        require!(amount > 0, EscrowError::InvalidAmount);
        require!(release_at > Clock::get()?.unix_timestamp, EscrowError::InvalidReleaseTime);
        require!(reference.len() <= Escrow::MAX_REFERENCE_LENGTH, EscrowError::ReferenceTooLong);

        let escrow = &mut ctx.accounts.escrow;
        escrow.payer = ctx.accounts.payer.key();
        escrow.merchant = ctx.accounts.merchant.key();
        escrow.beneficiary = ctx.accounts.beneficiary.key();
        escrow.arbiter = ctx.accounts.arbiter.as_ref().map(|arbiter| arbiter.key());
        escrow.amount = amount;
        escrow.release_at = release_at;
        escrow.status = EscrowStatus::Funded;
        escrow.reference = reference;
        escrow.bump = ctx.bumps.escrow;

        system_program::transfer(
            CpiContext::new(
                ctx.accounts.system_program.to_account_info(),
                system_program::Transfer {
                    from: ctx.accounts.payer.to_account_info(),
                    to: ctx.accounts.escrow.to_account_info(),
                },
            ),
            amount,
        )?;

        emit!(EscrowCreated {
            escrow: escrow.key(),
            payer: escrow.payer,
            merchant: escrow.merchant,
            beneficiary: escrow.beneficiary,
            amount,
            release_at,
        });
        Ok(())
    }

    pub fn release(ctx: Context<ReleaseEscrow>) -> Result<()> {
        let escrow = &ctx.accounts.escrow;
        require!(escrow.status == EscrowStatus::Funded, EscrowError::InvalidStatus);
        require!(
            Clock::get()?.unix_timestamp >= escrow.release_at,
            EscrowError::ReleaseTimeNotReached
        );

        let signer = ctx.accounts.authority.key();
        require!(
            signer == escrow.merchant
                || Some(signer) == escrow.arbiter
                || ctx.accounts.authority.key() == ctx.accounts.owner.key(),
            EscrowError::NotAuthorized
        );

        let seeds = &[
            b"escrow".as_ref(),
            escrow.payer.as_ref(),
            escrow.reference.as_bytes(),
            &[escrow.bump],
        ];
        system_program::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.system_program.to_account_info(),
                system_program::Transfer {
                    from: ctx.accounts.escrow.to_account_info(),
                    to: ctx.accounts.beneficiary.to_account_info(),
                },
                &[seeds],
            ),
            escrow.amount,
        )?;

        let escrow = &mut ctx.accounts.escrow;
        escrow.status = EscrowStatus::Released;
        emit!(EscrowReleased {
            escrow: escrow.key(),
            beneficiary: escrow.beneficiary,
            amount: escrow.amount,
        });
        Ok(())
    }

    pub fn refund(ctx: Context<RefundEscrow>) -> Result<()> {
        let escrow = &ctx.accounts.escrow;
        require!(escrow.status == EscrowStatus::Funded, EscrowError::InvalidStatus);

        let signer = ctx.accounts.authority.key();
        require!(
            signer == escrow.merchant || signer == escrow.payer,
            EscrowError::NotAuthorized
        );

        let seeds = &[
            b"escrow".as_ref(),
            escrow.payer.as_ref(),
            escrow.reference.as_bytes(),
            &[escrow.bump],
        ];
        system_program::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.system_program.to_account_info(),
                system_program::Transfer {
                    from: ctx.accounts.escrow.to_account_info(),
                    to: ctx.accounts.payer_account.to_account_info(),
                },
                &[seeds],
            ),
            escrow.amount,
        )?;

        let escrow = &mut ctx.accounts.escrow;
        escrow.status = EscrowStatus::Refunded;
        emit!(EscrowRefunded {
            escrow: escrow.key(),
            payer: escrow.payer,
            amount: escrow.amount,
        });
        Ok(())
    }

    pub fn dispute(ctx: Context<DisputeEscrow>) -> Result<()> {
        let escrow = &mut ctx.accounts.escrow;
        require!(escrow.status == EscrowStatus::Funded, EscrowError::InvalidStatus);
        let signer = ctx.accounts.authority.key();
        require!(
            signer == escrow.payer || signer == escrow.merchant,
            EscrowError::NotAuthorized
        );
        escrow.status = EscrowStatus::Disputed;
        emit!(EscrowDisputed { escrow: escrow.key(), initiator: signer });
        Ok(())
    }

    pub fn resolve(ctx: Context<ResolveEscrow>, pay_beneficiary: bool) -> Result<()> {
        let escrow = &ctx.accounts.escrow;
        require!(escrow.status == EscrowStatus::Disputed, EscrowError::InvalidStatus);
        require!(
            Some(ctx.accounts.authority.key()) == escrow.arbiter
                || ctx.accounts.authority.key() == ctx.accounts.owner.key(),
            EscrowError::NotAuthorized
        );

        let recipient = if pay_beneficiary {
            ctx.accounts.beneficiary.to_account_info()
        } else {
            ctx.accounts.payer_account.to_account_info()
        };
        let seeds = &[
            b"escrow".as_ref(),
            escrow.payer.as_ref(),
            escrow.reference.as_bytes(),
            &[escrow.bump],
        ];
        system_program::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.system_program.to_account_info(),
                system_program::Transfer {
                    from: ctx.accounts.escrow.to_account_info(),
                    to: recipient,
                },
                &[seeds],
            ),
            escrow.amount,
        )?;

        let escrow = &mut ctx.accounts.escrow;
        escrow.status = EscrowStatus::Resolved;
        Ok(())
    }
}

#[account]
pub struct Escrow {
    pub payer: Pubkey,
    pub merchant: Pubkey,
    pub beneficiary: Pubkey,
    pub arbiter: Option<Pubkey>,
    pub amount: u64,
    pub release_at: i64,
    pub status: EscrowStatus,
    pub reference: String,
    pub bump: u8,
}

impl Escrow {
    pub const MAX_REFERENCE_LENGTH: usize = 128;
    pub const LEN: usize = 8 + 32 + 32 + 32 + 33 + 8 + 8 + 1 + 4 + Self::MAX_REFERENCE_LENGTH + 1;
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, PartialEq, Eq)]
pub enum EscrowStatus {
    Funded,
    Released,
    Refunded,
    Disputed,
    Resolved,
}

#[derive(Accounts)]
#[instruction(amount: u64, release_at: i64, reference: String)]
pub struct InitializeEscrow<'info> {
    #[account(
        init,
        payer = payer,
        space = Escrow::LEN,
        seeds = [b"escrow", payer.key().as_ref(), reference.as_bytes()],
        bump
    )]
    pub escrow: Account<'info, Escrow>,
    #[account(mut)]
    pub payer: Signer<'info>,
    /// CHECK: beneficiary wallet, validated by the gateway before submission.
    #[account(mut)]
    pub beneficiary: SystemAccount<'info>,
    /// CHECK: merchant wallet, validated by the gateway before submission.
    pub merchant: SystemAccount<'info>,
    /// CHECK: optional arbiter wallet.
    pub arbiter: Option<SystemAccount<'info>>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct ReleaseEscrow<'info> {
    #[account(
        mut,
        seeds = [b"escrow", escrow.payer.as_ref(), escrow.reference.as_bytes()],
        bump = escrow.bump
    )]
    pub escrow: Account<'info, Escrow>,
    #[account(mut)]
    pub authority: Signer<'info>,
    /// CHECK: merchant beneficiary of the escrow.
    #[account(mut)]
    pub beneficiary: SystemAccount<'info>,
    /// CHECK: program owner.
    pub owner: SystemAccount<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct RefundEscrow<'info> {
    #[account(
        mut,
        seeds = [b"escrow", escrow.payer.as_ref(), escrow.reference.as_bytes()],
        bump = escrow.bump
    )]
    pub escrow: Account<'info, Escrow>,
    #[account(mut)]
    pub authority: Signer<'info>,
    /// CHECK: original payer receiving the refund.
    #[account(mut)]
    pub payer_account: SystemAccount<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct DisputeEscrow<'info> {
    #[account(
        mut,
        seeds = [b"escrow", escrow.payer.as_ref(), escrow.reference.as_bytes()],
        bump = escrow.bump
    )]
    pub escrow: Account<'info, Escrow>,
    pub authority: Signer<'info>,
}

#[derive(Accounts)]
pub struct ResolveEscrow<'info> {
    #[account(
        mut,
        seeds = [b"escrow", escrow.payer.as_ref(), escrow.reference.as_bytes()],
        bump = escrow.bump
    )]
    pub escrow: Account<'info, Escrow>,
    pub authority: Signer<'info>,
    /// CHECK: beneficiary wallet.
    #[account(mut)]
    pub beneficiary: SystemAccount<'info>,
    /// CHECK: payer wallet.
    #[account(mut)]
    pub payer_account: SystemAccount<'info>,
    /// CHECK: program owner.
    pub owner: SystemAccount<'info>,
    pub system_program: Program<'info, System>,
}

#[event]
pub struct EscrowCreated {
    pub escrow: Pubkey,
    pub payer: Pubkey,
    pub merchant: Pubkey,
    pub beneficiary: Pubkey,
    pub amount: u64,
    pub release_at: i64,
}

#[event]
pub struct EscrowReleased {
    pub escrow: Pubkey,
    pub beneficiary: Pubkey,
    pub amount: u64,
}

#[event]
pub struct EscrowRefunded {
    pub escrow: Pubkey,
    pub payer: Pubkey,
    pub amount: u64,
}

#[event]
pub struct EscrowDisputed {
    pub escrow: Pubkey,
    pub initiator: Pubkey,
}

#[error_code]
pub enum EscrowError {
    #[msg("amount must be greater than zero")]
    InvalidAmount,
    #[msg("release time must be in the future")]
    InvalidReleaseTime,
    #[msg("reference is too long")]
    ReferenceTooLong,
    #[msg("invalid escrow status")]
    InvalidStatus,
    #[msg("not authorized")]
    NotAuthorized,
    #[msg("release time has not been reached")]
    ReleaseTimeNotReached,
}
