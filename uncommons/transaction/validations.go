package transaction

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

var oneHundred = decimal.NewFromInt(100)

// BuildIntentPlan validates input allocations and builds a normalized intent plan.
func BuildIntentPlan(input TransactionIntentInput, status TransactionStatus) (IntentPlan, error) {
	if strings.TrimSpace(input.Asset) == "" {
		return IntentPlan{}, NewDomainError(ErrorInvalidInput, "asset", "asset is required")
	}

	if !input.Total.IsPositive() {
		return IntentPlan{}, NewDomainError(ErrorInvalidInput, "total", "total must be greater than zero")
	}

	if len(input.Sources) == 0 {
		return IntentPlan{}, NewDomainError(ErrorInvalidInput, "sources", "at least one source is required")
	}

	if len(input.Destinations) == 0 {
		return IntentPlan{}, NewDomainError(ErrorInvalidInput, "destinations", "at least one destination is required")
	}

	sources, err := buildPostings(input.Asset, input.Total, input.Pending, status, input.Sources, true)
	if err != nil {
		return IntentPlan{}, err
	}

	destinations, err := buildPostings(input.Asset, input.Total, input.Pending, status, input.Destinations, false)
	if err != nil {
		return IntentPlan{}, err
	}

	sourceTotal := sumPostings(sources)

	destinationTotal := sumPostings(destinations)
	if !sourceTotal.Equal(input.Total) || !destinationTotal.Equal(input.Total) {
		return IntentPlan{}, NewDomainError(
			ErrorTransactionValueMismatch,
			"total",
			fmt.Sprintf("source total=%s destination total=%s expected=%s", sourceTotal, destinationTotal, input.Total),
		)
	}

	sourceIDs := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		sourceIDs[source.Target.BalanceID] = struct{}{}
	}

	for _, destination := range destinations {
		if _, exists := sourceIDs[destination.Target.BalanceID]; exists {
			return IntentPlan{}, NewDomainError(ErrorTransactionAmbiguous, "destinations", "balance appears as source and destination")
		}
	}

	return IntentPlan{
		Asset:        input.Asset,
		Total:        input.Total,
		Pending:      input.Pending,
		Sources:      sources,
		Destinations: destinations,
	}, nil
}

// ValidateBalanceEligibility checks whether balances can participate in a plan.
func ValidateBalanceEligibility(plan IntentPlan, balances map[string]Balance) error {
	if len(balances) == 0 {
		return NewDomainError(ErrorAccountIneligibility, "balances", "balance catalog is empty")
	}

	for _, posting := range plan.Sources {
		balance, ok := balances[posting.Target.BalanceID]
		if !ok {
			return NewDomainError(ErrorAccountIneligibility, "sources", "source balance not found")
		}

		if balance.Asset != plan.Asset {
			return NewDomainError(ErrorAssetCodeNotFound, "sources", "source asset does not match transaction asset")
		}

		if !balance.AllowSending {
			return NewDomainError(ErrorAccountStatusTransactionRestriction, "sources", "source balance is not allowed to send")
		}

		if plan.Pending && balance.AccountType == AccountTypeExternal {
			return NewDomainError(ErrorOnHoldExternalAccount, "sources", "external source cannot be put on hold")
		}
	}

	for _, posting := range plan.Destinations {
		balance, ok := balances[posting.Target.BalanceID]
		if !ok {
			return NewDomainError(ErrorAccountIneligibility, "destinations", "destination balance not found")
		}

		if balance.Asset != plan.Asset {
			return NewDomainError(ErrorAssetCodeNotFound, "destinations", "destination asset does not match transaction asset")
		}

		if !balance.AllowReceiving {
			return NewDomainError(ErrorAccountStatusTransactionRestriction, "destinations", "destination balance is not allowed to receive")
		}

		if !balance.Available.IsZero() && balance.AccountType == AccountTypeExternal {
			return NewDomainError(ErrorInsufficientFunds, "destinations", "external destination must have zero available balance")
		}
	}

	return nil
}

// ApplyPosting applies a posting transition to a balance and returns the new state.
func ApplyPosting(balance Balance, posting Posting) (Balance, error) {
	if err := posting.Target.validate("posting.target"); err != nil {
		return Balance{}, err
	}

	if balance.ID != posting.Target.BalanceID {
		return Balance{}, NewDomainError(ErrorAccountIneligibility, "posting.target.balanceId", "posting does not belong to the provided balance")
	}

	if balance.AccountID != posting.Target.AccountID {
		return Balance{}, NewDomainError(ErrorAccountIneligibility, "posting.target.accountId", "posting account does not match balance account")
	}

	if balance.Asset != posting.Asset {
		return Balance{}, NewDomainError(ErrorAssetCodeNotFound, "posting.asset", "posting asset does not match balance asset")
	}

	if !posting.Amount.IsPositive() {
		return Balance{}, NewDomainError(ErrorInvalidInput, "posting.amount", "posting amount must be greater than zero")
	}

	result := balance

	switch posting.Operation {
	case OperationOnHold:
		if posting.Status != StatusPending {
			return Balance{}, NewDomainError(ErrorInvalidStateTransition, "posting.status", "ON_HOLD requires PENDING status")
		}

		result.Available = result.Available.Sub(posting.Amount)
		result.OnHold = result.OnHold.Add(posting.Amount)
	case OperationRelease:
		if posting.Status != StatusCanceled {
			return Balance{}, NewDomainError(ErrorInvalidStateTransition, "posting.status", "RELEASE requires CANCELED status")
		}

		result.OnHold = result.OnHold.Sub(posting.Amount)
		result.Available = result.Available.Add(posting.Amount)
	case OperationDebit:
		switch posting.Status {
		case StatusApproved:
			result.OnHold = result.OnHold.Sub(posting.Amount)
		case StatusCreated:
			result.Available = result.Available.Sub(posting.Amount)
		default:
			return Balance{}, NewDomainError(
				ErrorInvalidStateTransition,
				"posting.status",
				"DEBIT only supports CREATED or APPROVED status",
			)
		}
	case OperationCredit:
		switch posting.Status {
		case StatusCreated, StatusApproved, StatusPending:
			result.Available = result.Available.Add(posting.Amount)
		default:
			return Balance{}, NewDomainError(
				ErrorInvalidStateTransition,
				"posting.status",
				"CREDIT only supports CREATED, APPROVED, or PENDING status",
			)
		}
	default:
		return Balance{}, NewDomainError(ErrorInvalidInput, "posting.operation", "unsupported operation")
	}

	if result.Available.IsNegative() {
		return Balance{}, NewDomainError(ErrorInsufficientFunds, "posting.amount", "operation would result in negative available balance")
	}

	if result.OnHold.IsNegative() {
		return Balance{}, NewDomainError(ErrorInsufficientFunds, "posting.amount", "operation would result in negative on-hold balance")
	}

	result.Version++

	return result, nil
}

// ResolveOperation resolves the posting operation from pending/source/status semantics.
func ResolveOperation(pending bool, isSource bool, status TransactionStatus) (Operation, error) {
	if pending {
		switch status {
		case StatusPending:
			if isSource {
				return OperationOnHold, nil
			}

			return OperationCredit, nil
		case StatusCanceled:
			if isSource {
				return OperationRelease, nil
			}

			return OperationDebit, nil
		case StatusApproved:
			if isSource {
				return OperationDebit, nil
			}

			return OperationCredit, nil
		default:
			return "", NewDomainError(ErrorInvalidStateTransition, "status", "pending transactions only support PENDING, APPROVED, or CANCELED status")
		}
	}

	switch status {
	case StatusCreated:
		if isSource {
			return OperationDebit, nil
		}

		return OperationCredit, nil
	default:
		return "", NewDomainError(ErrorInvalidStateTransition, "status", "non-pending transactions only support CREATED status")
	}
}

func buildPostings(asset string, total decimal.Decimal, pending bool, status TransactionStatus, allocations []Allocation, isSource bool) ([]Posting, error) {
	postings := make([]Posting, len(allocations))
	allocated := decimal.Zero
	remainderIndex := -1

	for i, allocation := range allocations {
		field := fmt.Sprintf("allocations[%d]", i)
		if err := allocation.Target.validate(field + ".target"); err != nil {
			return nil, err
		}

		strategyCount := 0
		if allocation.Amount != nil {
			strategyCount++
		}

		if allocation.Share != nil {
			strategyCount++
		}

		if allocation.Remainder {
			strategyCount++
		}

		if strategyCount != 1 {
			return nil, NewDomainError(ErrorInvalidInput, field, "allocation must define exactly one strategy: amount, share, or remainder")
		}

		operation, err := ResolveOperation(pending, isSource, status)
		if err != nil {
			return nil, err
		}

		postings[i] = Posting{
			Target:    allocation.Target,
			Asset:     asset,
			Operation: operation,
			Status:    status,
			Route:     allocation.Route,
		}

		if allocation.Amount != nil {
			if !allocation.Amount.IsPositive() {
				return nil, NewDomainError(ErrorInvalidInput, field+".amount", "amount must be greater than zero")
			}

			postings[i].Amount = *allocation.Amount
			allocated = allocated.Add(*allocation.Amount)

			continue
		}

		if allocation.Share != nil {
			share := *allocation.Share
			if !share.IsPositive() || share.GreaterThan(oneHundred) {
				return nil, NewDomainError(ErrorInvalidInput, field+".share", "share must be greater than 0 and at most 100")
			}

			amount := total.Mul(share.Div(oneHundred))
			if !amount.IsPositive() {
				return nil, NewDomainError(ErrorInvalidInput, field+".share", "share produces a non-positive amount")
			}

			postings[i].Amount = amount
			allocated = allocated.Add(amount)

			continue
		}

		if remainderIndex >= 0 {
			return nil, NewDomainError(ErrorInvalidInput, field+".remainder", "only one remainder allocation is allowed")
		}

		remainderIndex = i
	}

	if remainderIndex >= 0 {
		remainder := total.Sub(allocated)
		if !remainder.IsPositive() {
			return nil, NewDomainError(ErrorTransactionValueMismatch, "allocations", "remainder is zero or negative")
		}

		postings[remainderIndex].Amount = remainder
		allocated = allocated.Add(remainder)
	}

	if !allocated.Equal(total) {
		return nil, NewDomainError(
			ErrorTransactionValueMismatch,
			"allocations",
			fmt.Sprintf("allocated=%s expected=%s", allocated, total),
		)
	}

	return postings, nil
}

func sumPostings(postings []Posting) decimal.Decimal {
	total := decimal.Zero

	for _, posting := range postings {
		total = total.Add(posting.Amount)
	}

	return total
}
