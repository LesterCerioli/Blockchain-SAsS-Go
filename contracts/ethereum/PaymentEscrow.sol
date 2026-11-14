// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title PaymentEscrow
/// @notice Minimal native-currency escrow used by the payment gateway settlement
///         layer. A payer funds an escrow bound to a merchant, a beneficiary and
///         a release timestamp. The merchant (or arbiter) can release funds to
///         the beneficiary after the release time, refund the payer before
///         release, or resolve a dispute. This contract is a reference
///         implementation and MUST be independently audited before production.
contract PaymentEscrow {
    enum EscrowStatus {
        None,
        Funded,
        Released,
        Refunded,
        Disputed,
        Resolved
    }

    struct Escrow {
        address payer;
        address payable merchant;
        address payable beneficiary;
        address arbiter;
        uint256 amount;
        uint64 releaseAt;
        uint64 createdAt;
        EscrowStatus status;
        string reference;
    }

    uint256 public constant MAX_REFERENCE_LENGTH = 128;

    mapping(bytes32 => Escrow) private escrows;
    mapping(address => bool) public pausedMerchants;

    address public owner;
    bool public paused;

    event EscrowCreated(
        bytes32 indexed escrowId,
        address indexed payer,
        address indexed merchant,
        address beneficiary,
        uint256 amount,
        uint64 releaseAt,
        string reference
    );
    event EscrowReleased(bytes32 indexed escrowId, address indexed beneficiary, uint256 amount);
    event EscrowRefunded(bytes32 indexed escrowId, address indexed payer, uint256 amount);
    event EscrowDisputed(bytes32 indexed escrowId, address indexed initiator);
    event EscrowResolved(bytes32 indexed escrowId, address indexed beneficiary, uint256 amount);
    event MerchantPaused(address indexed merchant, bool paused);

    error NotOwner();
    error ContractPaused();
    error MerchantPausedError();
    error InvalidAmount();
    error InvalidAddress();
    error InvalidReleaseTime();
    error ReferenceTooLong();
    error EscrowAlreadyExists();
    error EscrowNotFound();
    error InvalidStatus();
    error NotAuthorized();
    error ReleaseTimeNotReached();
    error TransferFailed();

    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    modifier whenNotPaused() {
        if (paused) revert ContractPaused();
        if (pausedMerchants[msg.sender]) revert MerchantPausedError();
        _;
    }

    constructor() {
        owner = msg.sender;
    }

    function createEscrow(
        bytes32 escrowId,
        address payable merchant,
        address payable beneficiary,
        address arbiter,
        uint64 releaseAt,
        string calldata reference
    ) external payable whenNotPaused {
        if (msg.value == 0) revert InvalidAmount();
        if (merchant == address(0) || beneficiary == address(0)) revert InvalidAddress();
        if (releaseAt <= block.timestamp) revert InvalidReleaseTime();
        if (bytes(reference).length > MAX_REFERENCE_LENGTH) revert ReferenceTooLong();
        if (escrows[escrowId].status != EscrowStatus.None) revert EscrowAlreadyExists();

        escrows[escrowId] = Escrow({
            payer: msg.sender,
            merchant: merchant,
            beneficiary: beneficiary,
            arbiter: arbiter,
            amount: msg.value,
            releaseAt: releaseAt,
            createdAt: uint64(block.timestamp),
            status: EscrowStatus.Funded,
            reference: reference
        });

        emit EscrowCreated(escrowId, msg.sender, merchant, beneficiary, msg.value, releaseAt, reference);
    }

    function release(bytes32 escrowId) external {
        Escrow storage escrow = _requireFunded(escrowId);
        if (msg.sender != escrow.merchant && msg.sender != escrow.arbiter && msg.sender != owner) {
            revert NotAuthorized();
        }
        if (block.timestamp < escrow.releaseAt) revert ReleaseTimeNotReached();

        escrow.status = EscrowStatus.Released;
        uint256 amount = escrow.amount;
        escrow.amount = 0;
        (bool ok, ) = escrow.beneficiary.call{value: amount}("");
        if (!ok) revert TransferFailed();
        emit EscrowReleased(escrowId, escrow.beneficiary, amount);
    }

    function refund(bytes32 escrowId) external {
        Escrow storage escrow = _requireFunded(escrowId);
        if (msg.sender != escrow.merchant && msg.sender != escrow.payer && msg.sender != owner) {
            revert NotAuthorized();
        }

        escrow.status = EscrowStatus.Refunded;
        uint256 amount = escrow.amount;
        escrow.amount = 0;
        (bool ok, ) = payable(escrow.payer).call{value: amount}("");
        if (!ok) revert TransferFailed();
        emit EscrowRefunded(escrowId, escrow.payer, amount);
    }

    function dispute(bytes32 escrowId) external {
        Escrow storage escrow = _requireFunded(escrowId);
        if (msg.sender != escrow.payer && msg.sender != escrow.merchant) revert NotAuthorized();
        escrow.status = EscrowStatus.Disputed;
        emit EscrowDisputed(escrowId, msg.sender);
    }

    function resolve(bytes32 escrowId, bool payBeneficiary) external {
        Escrow storage escrow = escrows[escrowId];
        if (escrow.status != EscrowStatus.Disputed) revert InvalidStatus();
        if (msg.sender != escrow.arbiter && msg.sender != owner) revert NotAuthorized();

        address payable recipient = payBeneficiary ? escrow.beneficiary : escrow.payer;
        escrow.status = EscrowStatus.Resolved;
        uint256 amount = escrow.amount;
        escrow.amount = 0;
        (bool ok, ) = recipient.call{value: amount}("");
        if (!ok) revert TransferFailed();
        emit EscrowResolved(escrowId, recipient, amount);
    }

    function getEscrow(bytes32 escrowId) external view returns (Escrow memory) {
        if (escrows[escrowId].status == EscrowStatus.None) revert EscrowNotFound();
        return escrows[escrowId];
    }

    function setMerchantPaused(address merchant, bool value) external onlyOwner {
        pausedMerchants[merchant] = value;
        emit MerchantPaused(merchant, value);
    }

    function setPaused(bool value) external onlyOwner {
        paused = value;
    }

    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert InvalidAddress();
        owner = newOwner;
    }

    function _requireFunded(bytes32 escrowId) private view returns (Escrow storage escrow) {
        escrow = escrows[escrowId];
        if (escrow.status != EscrowStatus.Funded) revert InvalidStatus();
    }
}
