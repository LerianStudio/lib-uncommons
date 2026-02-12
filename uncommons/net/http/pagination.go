// Package http provides shared HTTP helpers.
package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Pagination limits for API endpoints.
const (
	DefaultLimit  = 20
	DefaultOffset = 0
	MaxLimit      = 200
)

// ErrLimitMustBePositive is returned when limit is below 1.
var ErrLimitMustBePositive = errors.New("limit must be greater than zero")

// ErrOffsetMustBePositive is returned when offset is negative.
var ErrOffsetMustBePositive = errors.New("offset must be non-negative")

// ErrInvalidCursor is returned when the cursor cannot be decoded.
var ErrInvalidCursor = errors.New("invalid cursor format")

// ParsePagination parses limit/offset query params with defaults.
func ParsePagination(fiberCtx *fiber.Ctx) (int, int, error) {
	limit := DefaultLimit
	offset := DefaultOffset

	if limitValue := fiberCtx.Query("limit"); limitValue != "" {
		parsed, err := strconv.Atoi(limitValue)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid limit value: %w", err)
		}

		limit = parsed
	}

	if offsetValue := fiberCtx.Query("offset"); offsetValue != "" {
		parsed, err := strconv.Atoi(offsetValue)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid offset value: %w", err)
		}

		offset = parsed
	}

	if limit < 1 {
		return 0, 0, ErrLimitMustBePositive
	}

	if limit > MaxLimit {
		limit = MaxLimit
	}

	if offset < 0 {
		return 0, 0, ErrOffsetMustBePositive
	}

	return limit, offset, nil
}

// ParseOpaqueCursorPagination parses cursor/limit query params for opaque cursor pagination.
// It validates limit but does not attempt to decode the cursor string.
// Returns the raw cursor string (empty for first page), limit, and any error.
func ParseOpaqueCursorPagination(fiberCtx *fiber.Ctx) (string, int, error) {
	limit := DefaultLimit

	if limitValue := fiberCtx.Query("limit"); limitValue != "" {
		parsed, err := strconv.Atoi(limitValue)
		if err != nil {
			return "", 0, fmt.Errorf("invalid limit value: %w", err)
		}

		limit = parsed
	}

	if limit < 1 {
		return "", 0, ErrLimitMustBePositive
	}

	if limit > MaxLimit {
		limit = MaxLimit
	}

	cursorParam := fiberCtx.Query("cursor")
	if cursorParam == "" {
		return "", limit, nil
	}

	return cursorParam, limit, nil
}

// EncodeUUIDCursor encodes a UUID into a base64 cursor string.
// NOTE: Named EncodeUUIDCursor to avoid collision with the existing
// CreateCursor/DecodeCursor in cursor.go which use Cursor struct.
func EncodeUUIDCursor(id uuid.UUID) string {
	return base64.StdEncoding.EncodeToString([]byte(id.String()))
}

// DecodeUUIDCursor decodes a base64 cursor string into a UUID.
// NOTE: Named DecodeUUIDCursor to avoid collision with the existing
// DecodeCursor in cursor.go which returns a Cursor struct.
func DecodeUUIDCursor(cursor string) (uuid.UUID, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: decode failed: %w", ErrInvalidCursor, err)
	}

	id, err := uuid.Parse(string(decoded))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: parse failed: %w", ErrInvalidCursor, err)
	}

	return id, nil
}

// TimestampCursor represents a cursor for keyset pagination with timestamp + ID ordering.
// This ensures correct pagination when records are ordered by (timestamp DESC, id DESC).
type TimestampCursor struct {
	Timestamp time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

// EncodeTimestampCursor encodes a timestamp and UUID into a base64 cursor string.
func EncodeTimestampCursor(timestamp time.Time, id uuid.UUID) string {
	cursor := TimestampCursor{
		Timestamp: timestamp.UTC(),
		ID:        id,
	}

	data, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString(data)
}

// DecodeTimestampCursor decodes a base64 cursor string into a TimestampCursor.
func DecodeTimestampCursor(cursor string) (*TimestampCursor, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: decode failed: %w", ErrInvalidCursor, err)
	}

	var tc TimestampCursor
	if err := json.Unmarshal(decoded, &tc); err != nil {
		return nil, fmt.Errorf("%w: unmarshal failed: %w", ErrInvalidCursor, err)
	}

	if tc.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: missing id", ErrInvalidCursor)
	}

	return &tc, nil
}

// ParseTimestampCursorPagination parses cursor/limit query params for timestamp-based cursor pagination.
// Returns the decoded TimestampCursor (nil for first page), limit, and any error.
func ParseTimestampCursorPagination(fiberCtx *fiber.Ctx) (*TimestampCursor, int, error) {
	limit := DefaultLimit

	if limitValue := fiberCtx.Query("limit"); limitValue != "" {
		parsed, err := strconv.Atoi(limitValue)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid limit value: %w", err)
		}

		limit = parsed
	}

	if limit < 1 {
		return nil, 0, ErrLimitMustBePositive
	}

	if limit > MaxLimit {
		limit = MaxLimit
	}

	cursorParam := fiberCtx.Query("cursor")
	if cursorParam == "" {
		return nil, limit, nil
	}

	tc, err := DecodeTimestampCursor(cursorParam)
	if err != nil {
		return nil, 0, err
	}

	return tc, limit, nil
}

// SortCursor encodes a position in a sorted result set for composite keyset pagination.
// It stores the sort column name, sort value, and record ID, enabling stable cursor
// pagination when ordering by columns other than id.
type SortCursor struct {
	SortColumn string `json:"sc"`
	SortValue  string `json:"sv"`
	ID         string `json:"i"`
	PointsNext bool   `json:"pn"`
}

// EncodeSortCursor encodes sort cursor data into a base64 string.
func EncodeSortCursor(sortColumn, sortValue, id string, pointsNext bool) string {
	cursor := SortCursor{
		SortColumn: sortColumn,
		SortValue:  sortValue,
		ID:         id,
		PointsNext: pointsNext,
	}

	data, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString(data)
}

// DecodeSortCursor decodes a base64 cursor string into a SortCursor.
func DecodeSortCursor(cursor string) (*SortCursor, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: decode failed: %w", ErrInvalidCursor, err)
	}

	var sc SortCursor
	if err := json.Unmarshal(decoded, &sc); err != nil {
		return nil, fmt.Errorf("%w: unmarshal failed: %w", ErrInvalidCursor, err)
	}

	if sc.ID == "" {
		return nil, fmt.Errorf("%w: missing id", ErrInvalidCursor)
	}

	return &sc, nil
}

// SortCursorDirection computes the actual SQL ORDER BY direction and comparison
// operator for composite keyset pagination based on the requested direction and
// whether the cursor points forward or backward.
func SortCursorDirection(requestedDir string, pointsNext bool) (actualDir, operator string) {
	isAsc := strings.EqualFold(requestedDir, SortDirASC)

	if pointsNext {
		if isAsc {
			return SortDirASC, ">"
		}

		return SortDirDESC, "<"
	}

	// Backward navigation: flip the direction
	if isAsc {
		return SortDirDESC, "<"
	}

	return SortDirASC, ">"
}

// CalculateSortCursorPagination computes Next/Prev cursor strings for composite keyset pagination.
func CalculateSortCursorPagination(
	isFirstPage, hasPagination, pointsNext bool,
	sortColumn string,
	firstSortValue, firstID string,
	lastSortValue, lastID string,
) (next, prev string) {
	hasNext := (pointsNext && hasPagination) || (!pointsNext && (hasPagination || isFirstPage))

	if hasNext {
		next = EncodeSortCursor(sortColumn, lastSortValue, lastID, true)
	}

	if !isFirstPage {
		prev = EncodeSortCursor(sortColumn, firstSortValue, firstID, false)
	}

	return next, prev
}

// ValidateSortColumn checks whether column is in the allowed list (case-insensitive)
// and returns the matched allowed value. If no match is found, it returns defaultColumn.
func ValidateSortColumn(column string, allowed []string, defaultColumn string) string {
	for _, a := range allowed {
		if strings.EqualFold(column, a) {
			return a
		}
	}

	return defaultColumn
}
