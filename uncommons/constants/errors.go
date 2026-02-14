package constant

import "errors"

var (
	// ErrInsufficientFunds maps to transaction error code 0018.
	ErrInsufficientFunds = errors.New("0018")
	// ErrAccountIneligibility maps to transaction error code 0019.
	ErrAccountIneligibility = errors.New("0019")
	// ErrAccountStatusTransactionRestriction maps to transaction error code 0024.
	ErrAccountStatusTransactionRestriction = errors.New("0024")
	// ErrAssetCodeNotFound maps to transaction error code 0034.
	ErrAssetCodeNotFound = errors.New("0034")
	// ErrTransactionValueMismatch maps to transaction error code 0073.
	ErrTransactionValueMismatch = errors.New("0073")
	// ErrTransactionAmbiguous maps to transaction error code 0090.
	ErrTransactionAmbiguous = errors.New("0090")
	// ErrOverFlowInt64 maps to transaction error code 0097.
	ErrOverFlowInt64 = errors.New("0097")
	// ErrOnHoldExternalAccount maps to transaction error code 0098.
	ErrOnHoldExternalAccount = errors.New("0098")
)
