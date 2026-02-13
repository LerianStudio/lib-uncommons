package transaction

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/LerianStudio/lib-uncommons/uncommons"
	constant "github.com/LerianStudio/lib-uncommons/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/uncommons/opentelemetry"
	"github.com/shopspring/decimal"
)

// Deprecated: use ValidateBalancesRules method from Midaz pkg instead.
// ValidateBalancesRules function with some validates in accounts and DSL operations
func ValidateBalancesRules(ctx context.Context, transaction Transaction, validate Responses, balances []*Balance) error {
	logger, tracer, _, _ := uncommons.NewTrackingFromContext(ctx)

	_, spanValidateBalances := tracer.Start(ctx, "validations.validate_balances_rules")
	defer spanValidateBalances.End()

	if len(balances) != (len(validate.From) + len(validate.To)) {
		err := uncommons.ValidateBusinessError(constant.ErrAccountIneligibility, "ValidateAccounts")

		opentelemetry.HandleSpanBusinessErrorEvent(&spanValidateBalances, "validations.validate_balances_rules", err)

		return err
	}

	for _, balance := range balances {
		if balance == nil {
			err := uncommons.ValidateBusinessError(constant.ErrAccountIneligibility, "ValidateBalancesRules")

			opentelemetry.HandleSpanBusinessErrorEvent(&spanValidateBalances, "validations.validate_balances_rules", err)

			return err
		}

		if err := validateFromBalances(balance, validate.From, validate.Asset, validate.Pending); err != nil {
			opentelemetry.HandleSpanBusinessErrorEvent(&spanValidateBalances, "validations.validate_from_balances_", err)

			logger.Errorf("validations.validate_from_balances_err: %s", err)

			return err
		}

		if err := validateToBalances(balance, validate.To, validate.Asset); err != nil {
			opentelemetry.HandleSpanBusinessErrorEvent(&spanValidateBalances, "validations.validate_to_balances_", err)

			logger.Errorf("validations.validate_to_balances_err: %s", err)

			return err
		}
	}

	return nil
}

func validateFromBalances(balance *Balance, from map[string]Amount, asset string, pending bool) error {
	if balance == nil {
		return uncommons.ValidateBusinessError(constant.ErrAccountIneligibility, "validateFromAccounts")
	}

	for key := range from {
		balanceAliasKey := AliasKey(balance.Alias, balance.Key)
		if key == balance.ID || SplitAliasWithKey(key) == balanceAliasKey {
			if balance.AssetCode != asset {
				return uncommons.ValidateBusinessError(constant.ErrAssetCodeNotFound, "validateFromAccounts")
			}

			if !balance.AllowSending {
				return uncommons.ValidateBusinessError(constant.ErrAccountStatusTransactionRestriction, "validateFromAccounts")
			}

			if pending && balance.AccountType == constant.ExternalAccountType {
				return uncommons.ValidateBusinessError(constant.ErrOnHoldExternalAccount, "validateBalance", balance.Alias)
			}
		}
	}

	return nil
}

func validateToBalances(balance *Balance, to map[string]Amount, asset string) error {
	if balance == nil {
		return uncommons.ValidateBusinessError(constant.ErrAccountIneligibility, "validateToAccounts")
	}

	balanceAliasKey := AliasKey(balance.Alias, balance.Key)
	for key := range to {
		if key == balance.ID || SplitAliasWithKey(key) == balanceAliasKey {
			if balance.AssetCode != asset {
				return uncommons.ValidateBusinessError(constant.ErrAssetCodeNotFound, "validateToAccounts")
			}

			if !balance.AllowReceiving {
				return uncommons.ValidateBusinessError(constant.ErrAccountStatusTransactionRestriction, "validateToAccounts")
			}

			if balance.Available.IsPositive() && balance.AccountType == constant.ExternalAccountType {
				return uncommons.ValidateBusinessError(constant.ErrInsufficientFunds, "validateToAccounts", balance.Alias)
			}
		}
	}

	return nil
}

// Deprecated: use ValidateFromToOperation method from Midaz pkg instead.
// ValidateFromToOperation func that validate operate balance
func ValidateFromToOperation(ft FromTo, validate Responses, balance *Balance) (Amount, Balance, error) {
	if balance == nil {
		return Amount{}, Balance{}, uncommons.ValidateBusinessError(constant.ErrAccountIneligibility, "ValidateFromToOperation", ft.AccountAlias)
	}

	side := validate.From
	if !ft.IsFrom {
		side = validate.To
	}

	amount, ok := operationAmountByKey(ft, side, balance.ID)
	if !ok {
		return Amount{}, Balance{}, uncommons.ValidateBusinessError(constant.ErrAccountIneligibility, "ValidateFromToOperation", ft.AccountAlias)
	}

	if ft.IsFrom {
		ba, err := OperateBalances(amount, *balance)
		if err != nil {
			return Amount{}, Balance{}, err
		}

		if ba.Available.IsNegative() && balance.AccountType != constant.ExternalAccountType {
			return Amount{}, Balance{}, uncommons.ValidateBusinessError(constant.ErrInsufficientFunds, "ValidateFromToOperation", balance.Alias)
		}

		return amount, ba, nil
	} else {
		ba, err := OperateBalances(amount, *balance)
		if err != nil {
			return Amount{}, Balance{}, err
		}

		return amount, ba, nil
	}
}

func operationAmountByKey(ft FromTo, data map[string]Amount, candidateKeys ...string) (Amount, bool) {
	if data == nil {
		return Amount{}, false
	}

	aliasCandidates := []string{
		ft.AccountAlias,
		AliasKey(ft.AccountAlias, ft.BalanceKey),
		AliasKey(ft.SplitAlias(), ft.BalanceKey),
	}

	if strings.TrimSpace(ft.BalanceKey) == "" {
		aliasCandidates = append(aliasCandidates, ft.SplitAlias())
	}

	aliasCandidates = append(aliasCandidates, candidateKeys...)

	seen := map[string]struct{}{}

	for _, key := range aliasCandidates {
		if key == "" {
			continue
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}

		if amount, ok := data[key]; ok {
			return amount, true
		}
	}

	return Amount{}, false
}

// Deprecated: use AliasKey method from Midaz pkg instead.
// AliasKey function to concatenate alias with balance key
func AliasKey(alias, balanceKey string) string {
	if balanceKey == "" {
		balanceKey = "default"
	}

	return alias + "#" + balanceKey
}

// Deprecated: use SplitAlias method from Midaz pkg instead.
// SplitAlias function to split alias with index
func SplitAlias(alias string) string {
	if strings.Contains(alias, "#") {
		return strings.Split(alias, "#")[1]
	}

	return alias
}

// Deprecated: use ConcatAlias method from Midaz pkg instead.
// ConcatAlias function to concat alias with index
func ConcatAlias(i int, alias string) string {
	return strconv.Itoa(i) + "#" + alias
}

// Deprecated: use OperateBalances method from Midaz pkg instead.
// OperateBalances Function to sum or sub two balances and Normalize the scale
func OperateBalances(amount Amount, balance Balance) (Balance, error) {
	var (
		total        decimal.Decimal
		totalOnHold  decimal.Decimal
		totalVersion int64
	)

	result := balance

	total = balance.Available
	totalOnHold = balance.OnHold

	switch {
	case amount.Operation == constant.ONHOLD && amount.TransactionType == constant.PENDING:
		total = balance.Available.Sub(amount.Value)
		totalOnHold = balance.OnHold.Add(amount.Value)
	case amount.Operation == constant.RELEASE && amount.TransactionType == constant.CANCELED:
		totalOnHold = balance.OnHold.Sub(amount.Value)
		total = balance.Available.Add(amount.Value)
	case amount.Operation == constant.DEBIT && amount.TransactionType == constant.APPROVED:
		totalOnHold = balance.OnHold.Sub(amount.Value)
	case amount.Operation == constant.CREDIT && amount.TransactionType == constant.APPROVED:
		total = balance.Available.Add(amount.Value)
	case amount.Operation == constant.DEBIT && amount.TransactionType == constant.CREATED:
		total = balance.Available.Sub(amount.Value)
	case amount.Operation == constant.CREDIT && amount.TransactionType == constant.CREATED:
		total = balance.Available.Add(amount.Value)
	default:
		return Balance{}, fmt.Errorf("invalid operation or transaction type: operation=%s, transactionType=%s", amount.Operation, amount.TransactionType)
	}

	totalVersion = balance.Version + 1

	result.Available = total
	result.OnHold = totalOnHold
	result.Version = totalVersion

	return result, nil
}

// Deprecated: use DetermineOperation method from Midaz pkg instead.
// DetermineOperation Function to determine the operation
func DetermineOperation(isPending bool, isFrom bool, transactionType string) string {
	switch {
	case isPending && transactionType == constant.PENDING:
		switch {
		case isFrom:
			return constant.ONHOLD
		default:
			return constant.CREDIT
		}
	case isPending && isFrom && transactionType == constant.CANCELED:
		return constant.RELEASE
	case isPending && !isFrom && transactionType == constant.CANCELED:
		return constant.DEBIT
	case isPending && transactionType == constant.APPROVED:
		switch {
		case isFrom:
			return constant.DEBIT
		default:
			return constant.CREDIT
		}
	case !isPending:
		switch {
		case isFrom:
			return constant.DEBIT
		default:
			return constant.CREDIT
		}
	default:
		return constant.CREDIT
	}
}

// Deprecated: use CalculateTotal method from Midaz pkg instead.
// CalculateTotal Calculate total for sources/destinations based on shares, amounts and remains
func CalculateTotal(fromTos []FromTo, transaction Transaction, transactionType string, t chan decimal.Decimal, ft chan map[string]Amount, sd chan []string, or chan map[string]string) {
	fmto := make(map[string]Amount)
	scdt := make([]string, 0, len(fromTos))

	total := decimal.NewFromInt(0)

	remaining := Amount{
		Asset:           transaction.Send.Asset,
		Value:           transaction.Send.Value,
		TransactionType: transactionType,
	}

	operationRoute := make(map[string]string)

	for i := range fromTos {
		aliasKey := AliasKey(fromTos[i].SplitAlias(), fromTos[i].BalanceKey)
		operationRoute[aliasKey] = fromTos[i].Route

		operation := DetermineOperation(transaction.Pending, fromTos[i].IsFrom, transactionType)

		addAmount := func(a Amount) {
			existing, ok := fmto[aliasKey]
			if !ok {
				fmto[aliasKey] = a
				return
			}

			existing.Value = existing.Value.Add(a.Value)
			fmto[aliasKey] = existing
		}

		if fromTos[i].Share != nil && fromTos[i].Share.Percentage != 0 {
			oneHundred := decimal.NewFromInt(100)

			percentage := decimal.NewFromInt(fromTos[i].Share.Percentage)

			percentageOfPercentage := decimal.NewFromInt(fromTos[i].Share.PercentageOfPercentage)
			if percentageOfPercentage.IsZero() {
				percentageOfPercentage = oneHundred
			}

			firstPart := percentage.Div(oneHundred)
			secondPart := percentageOfPercentage.Div(oneHundred)
			shareValue := transaction.Send.Value.Mul(firstPart).Mul(secondPart)

			addAmount(Amount{
				Asset:           transaction.Send.Asset,
				Value:           shareValue,
				Operation:       operation,
				TransactionType: transactionType,
			})

			total = total.Add(shareValue)
			remaining.Value = remaining.Value.Sub(shareValue)
		}

		if fromTos[i].Amount != nil && fromTos[i].Amount.Value.IsPositive() {
			amount := Amount{
				Asset:           fromTos[i].Amount.Asset,
				Value:           fromTos[i].Amount.Value,
				Operation:       operation,
				TransactionType: transactionType,
			}

			addAmount(amount)
			total = total.Add(amount.Value)

			remaining.Value = remaining.Value.Sub(amount.Value)
		}

		if !uncommons.IsNilOrEmpty(&fromTos[i].Remaining) {
			total = total.Add(remaining.Value)

			remaining.Operation = operation

			addAmount(remaining)
			fromTos[i].Amount = &remaining
		}

		scdt = append(scdt, aliasKey)
	}

	t <- total

	ft <- fmto

	sd <- scdt

	or <- operationRoute
}

// Deprecated: use AppendIfNotExist method from Midaz pkg instead.
// AppendIfNotExist Append if not exist
func AppendIfNotExist(slice []string, s []string) []string {
	for _, v := range s {
		if !uncommons.Contains(slice, v) {
			slice = append(slice, v)
		}
	}

	return slice
}

// Deprecated: use ValidateSendSourceAndDistribute method from Midaz pkg instead.
// ValidateSendSourceAndDistribute Validate send and distribute totals
func ValidateSendSourceAndDistribute(ctx context.Context, transaction Transaction, transactionType string) (*Responses, error) {
	var (
		sourcesTotal      decimal.Decimal
		destinationsTotal decimal.Decimal
	)

	logger, tracer, _, _ := uncommons.NewTrackingFromContext(ctx)

	_, span := tracer.Start(ctx, "uncommons.transaction.ValidateSendSourceAndDistribute")
	defer span.End()

	sizeFrom := len(transaction.Send.Source.From)
	sizeTo := len(transaction.Send.Distribute.To)

	response := &Responses{
		Total:               transaction.Send.Value,
		Asset:               transaction.Send.Asset,
		From:                make(map[string]Amount, sizeFrom),
		To:                  make(map[string]Amount, sizeTo),
		Sources:             make([]string, 0, sizeFrom),
		Destinations:        make([]string, 0, sizeTo),
		Aliases:             make([]string, 0, sizeFrom+sizeTo),
		Pending:             transaction.Pending,
		TransactionRoute:    transaction.Route,
		OperationRoutesFrom: make(map[string]string, sizeFrom),
		OperationRoutesTo:   make(map[string]string, sizeTo),
	}

	tFrom := make(chan decimal.Decimal, sizeFrom)
	ftFrom := make(chan map[string]Amount, sizeFrom)
	sdFrom := make(chan []string, sizeFrom)
	orFrom := make(chan map[string]string, sizeFrom)

	go CalculateTotal(transaction.Send.Source.From, transaction, transactionType, tFrom, ftFrom, sdFrom, orFrom)

	sourcesTotal = <-tFrom
	response.From = <-ftFrom
	response.Sources = <-sdFrom
	response.OperationRoutesFrom = <-orFrom
	response.Aliases = AppendIfNotExist(response.Aliases, response.Sources)

	tTo := make(chan decimal.Decimal, sizeTo)
	ftTo := make(chan map[string]Amount, sizeTo)
	sdTo := make(chan []string, sizeTo)
	orTo := make(chan map[string]string, sizeTo)

	go CalculateTotal(transaction.Send.Distribute.To, transaction, transactionType, tTo, ftTo, sdTo, orTo)

	destinationsTotal = <-tTo
	response.To = <-ftTo
	response.Destinations = <-sdTo
	response.OperationRoutesTo = <-orTo
	response.Aliases = AppendIfNotExist(response.Aliases, response.Destinations)

	for _, source := range response.Sources {
		if _, ok := response.To[source]; ok {
			logger.Errorf("ValidateSendSourceAndDistribute: Ambiguous transaction source and destination")

			return nil, uncommons.ValidateBusinessError(constant.ErrTransactionAmbiguous, "ValidateSendSourceAndDistribute")
		}
	}

	for _, destination := range response.Destinations {
		if _, ok := response.From[destination]; ok {
			logger.Errorf("ValidateSendSourceAndDistribute: Ambiguous transaction source and destination")

			return nil, uncommons.ValidateBusinessError(constant.ErrTransactionAmbiguous, "ValidateSendSourceAndDistribute")
		}
	}

	if !sourcesTotal.Equal(destinationsTotal) || !destinationsTotal.Equal(response.Total) {
		logger.Errorf("ValidateSendSourceAndDistribute: Transaction value mismatch")

		return nil, uncommons.ValidateBusinessError(constant.ErrTransactionValueMismatch, "ValidateSendSourceAndDistribute")
	}

	return response, nil
}
