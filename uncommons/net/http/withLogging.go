package http

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LerianStudio/lib-uncommons/uncommons"
	cn "github.com/LerianStudio/lib-uncommons/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/uncommons/security"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// maxObfuscationDepth limits recursion depth when obfuscating nested JSON structures
// to prevent stack overflow on deeply nested or malicious payloads.
const maxObfuscationDepth = 32

// logObfuscationDisabled caches the LOG_OBFUSCATION_DISABLED env var at init time
// to avoid repeated syscalls on every request.
var logObfuscationDisabled = os.Getenv("LOG_OBFUSCATION_DISABLED") == "true"

// RequestInfo is a struct design to store http access log data.
type RequestInfo struct {
	Method        string
	Username      string
	URI           string
	Referer       string
	RemoteAddress string
	Status        int
	Date          time.Time
	Duration      time.Duration
	UserAgent     string
	TraceID       string
	Protocol      string
	Size          int
	Body          string
}

// ResponseMetricsWrapper is a Wrapper responsible for collect the response data such as status code and size
// It implements built-in ResponseWriter interface.
type ResponseMetricsWrapper struct {
	Context    *fiber.Ctx
	StatusCode int
	Size       int
	Body       string
}

// NewRequestInfo creates an instance of RequestInfo.
func NewRequestInfo(c *fiber.Ctx) *RequestInfo {
	username, referer := "-", "-"
	rawURL := string(c.Request().URI().FullURI())

	parsedURL, err := url.Parse(rawURL)
	if err == nil && parsedURL.User != nil {
		if name := parsedURL.User.Username(); name != "" {
			username = name
		}
	}

	if c.Get("Referer") != "" {
		referer = c.Get("Referer")
	}

	body := ""

	if c.Request().Header.ContentLength() > 0 {
		bodyBytes := c.Body()

		if !logObfuscationDisabled {
			body = getBodyObfuscatedString(c, bodyBytes)
		} else {
			body = string(bodyBytes)
		}
	}

	return &RequestInfo{
		TraceID:       c.Get(cn.HeaderID),
		Method:        c.Method(),
		URI:           c.OriginalURL(),
		Username:      username,
		Referer:       referer,
		UserAgent:     c.Get(cn.HeaderUserAgent),
		RemoteAddress: c.IP(),
		Protocol:      c.Protocol(),
		Date:          time.Now().UTC(),
		Body:          body,
	}
}

// CLFString produces a log entry format similar to Common Log Format (CLF)
// Ref: https://httpd.apache.org/docs/trunk/logs.html#common
func (r *RequestInfo) CLFString() string {
	return strings.Join([]string{
		r.RemoteAddress,
		"-",
		r.Username,
		r.Protocol,
		r.Date.Format("[02/Jan/2006:15:04:05 -0700]"),
		`"` + r.Method + " " + r.URI + `"`,
		strconv.Itoa(r.Status),
		strconv.Itoa(r.Size),
		r.Referer,
		r.UserAgent,
	}, " ")
}

// String implements fmt.Stringer interface and produces a log entry using RequestInfo.CLFExtendedString.
func (r *RequestInfo) String() string {
	return r.CLFString()
}

// FinishRequestInfo calculates the duration of RequestInfo automatically using time.Now()
// It also set StatusCode and Size of RequestInfo passed by ResponseMetricsWrapper.
func (r *RequestInfo) FinishRequestInfo(rw *ResponseMetricsWrapper) {
	r.Duration = time.Now().UTC().Sub(r.Date)
	r.Status = rw.StatusCode
	r.Size = rw.Size
}

type logMiddleware struct {
	Logger log.Logger
}

// LogMiddlewareOption represents the log middleware function as an implementation.
type LogMiddlewareOption func(l *logMiddleware)

// WithCustomLogger is a functional option for logMiddleware.
func WithCustomLogger(logger log.Logger) LogMiddlewareOption {
	return func(l *logMiddleware) {
		if logger != nil {
			l.Logger = logger
		}
	}
}

// buildOpts creates an instance of logMiddleware with options.
func buildOpts(opts ...LogMiddlewareOption) *logMiddleware {
	mid := &logMiddleware{
		Logger: &log.GoLogger{},
	}

	for _, opt := range opts {
		opt(mid)
	}

	return mid
}

// WithHTTPLogging is a middleware to log access to http server.
// It logs access log according to Apache Standard Logs which uses Common Log Format (CLF)
// Ref: https://httpd.apache.org/docs/trunk/logs.html#common
func WithHTTPLogging(opts ...LogMiddlewareOption) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Path() == "/health" {
			return c.Next()
		}

		if strings.Contains(c.Path(), "swagger") && c.Path() != "/swagger/index.html" {
			return c.Next()
		}

		setRequestHeaderID(c)

		info := NewRequestInfo(c)

		headerID := c.Get(cn.HeaderID)

		mid := buildOpts(opts...)
		logger := mid.Logger.
			With(log.String(cn.HeaderID, info.TraceID)).
			With(log.String("message_prefix", headerID+cn.LoggerDefaultSeparator))

		ctx := uncommons.ContextWithLogger(c.UserContext(), logger)
		c.SetUserContext(ctx)

		err := c.Next()

		rw := ResponseMetricsWrapper{
			Context:    c,
			StatusCode: c.Response().StatusCode(),
			Size:       len(c.Response().Body()),
			Body:       "",
		}

		info.FinishRequestInfo(&rw)

		logger.Log(c.UserContext(), log.LevelInfo, info.CLFString())

		return err
	}
}

// WithGrpcLogging is a gRPC unary interceptor to log access to gRPC server.
func WithGrpcLogging(opts ...LogMiddlewareOption) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		// Prefer request_id from the gRPC request body when available and valid.
		if rid, ok := getValidBodyRequestID(req); ok {
			// Emit a debug log if overriding a different metadata id
			if prev := getMetadataID(ctx); prev != "" && prev != rid {
				mid := buildOpts(opts...)
				mid.Logger.Log(ctx, log.LevelDebug, "Overriding correlation id from metadata with body request_id",
					log.String("metadata_id", prev),
					log.String("body_request_id", rid),
				)
			}
			// Override correlation id to match the body-provided, validated UUID request_id
			ctx = uncommons.ContextWithHeaderID(ctx, rid)
			// Ensure standardized span attribute is present
			ctx = uncommons.ContextWithSpanAttributes(ctx, attribute.String("app.request.request_id", rid))
		} else {
			// Fallback to metadata path only if body is empty/invalid or accessor not present
			ctx = setGRPCRequestHeaderID(ctx)
		}

		_, _, reqId, _ := uncommons.NewTrackingFromContext(ctx)

		mid := buildOpts(opts...)
		logger := mid.Logger.
			With(log.String(cn.HeaderID, reqId)).
			With(log.String("message_prefix", reqId+cn.LoggerDefaultSeparator))

		ctx = uncommons.ContextWithLogger(ctx, logger)

		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		fields := []log.Field{
			log.String("method", info.FullMethod),
			log.String("duration", duration.String()),
		}
		if err != nil {
			fields = append(fields, log.Any("error", err))
		}

		logger.Log(ctx, log.LevelInfo, "gRPC request finished", fields...)

		return resp, err
	}
}

func setRequestHeaderID(c *fiber.Ctx) {
	headerID := c.Get(cn.HeaderID)

	if uncommons.IsNilOrEmpty(&headerID) {
		headerID = uuid.New().String()
		c.Set(cn.HeaderID, headerID)
		c.Request().Header.Set(cn.HeaderID, headerID)
		c.Response().Header.Set(cn.HeaderID, headerID)
	}

	ctx := uncommons.ContextWithHeaderID(c.UserContext(), headerID)
	c.SetUserContext(ctx)
}

func setGRPCRequestHeaderID(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		headerID := md.Get(cn.MetadataID)
		if len(headerID) > 0 && !uncommons.IsNilOrEmpty(&headerID[0]) {
			return uncommons.ContextWithHeaderID(ctx, headerID[0])
		}
	}

	// If metadata is not present, or if the header ID is missing or empty, generate a new one.
	return uncommons.ContextWithHeaderID(ctx, uuid.New().String())
}

func getBodyObfuscatedString(c *fiber.Ctx, bodyBytes []byte) string {
	contentType := c.Get("Content-Type")

	var obfuscatedBody string

	if strings.Contains(contentType, "application/json") {
		obfuscatedBody = handleJSONBody(bodyBytes)
	} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		obfuscatedBody = handleURLEncodedBody(bodyBytes)
	} else if strings.Contains(contentType, "multipart/form-data") {
		obfuscatedBody = handleMultipartBody(c)
	} else {
		obfuscatedBody = string(bodyBytes)
	}

	return obfuscatedBody
}

func handleJSONBody(bodyBytes []byte) string {
	var bodyData map[string]any
	if err := json.Unmarshal(bodyBytes, &bodyData); err != nil {
		return string(bodyBytes)
	}

	obfuscateMapRecursively(bodyData, 0)

	updatedBody, err := json.Marshal(bodyData)
	if err != nil {
		return string(bodyBytes)
	}

	return string(updatedBody)
}

func obfuscateMapRecursively(data map[string]any, depth int) {
	if depth >= maxObfuscationDepth {
		return
	}

	for key, value := range data {
		if security.IsSensitiveField(key) {
			data[key] = cn.ObfuscatedValue
			continue
		}

		switch v := value.(type) {
		case map[string]any:
			obfuscateMapRecursively(v, depth+1)
		case []any:
			obfuscateSliceRecursively(v, depth+1)
		}
	}
}

func obfuscateSliceRecursively(data []any, depth int) {
	if depth >= maxObfuscationDepth {
		return
	}

	for _, item := range data {
		switch v := item.(type) {
		case map[string]any:
			obfuscateMapRecursively(v, depth+1)
		case []any:
			obfuscateSliceRecursively(v, depth+1)
		}
	}
}

func handleURLEncodedBody(bodyBytes []byte) string {
	formData, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		return string(bodyBytes)
	}

	updatedBody := url.Values{}

	for key, values := range formData {
		if security.IsSensitiveField(key) {
			for range values {
				updatedBody.Add(key, cn.ObfuscatedValue)
			}
		} else {
			for _, value := range values {
				updatedBody.Add(key, value)
			}
		}
	}

	return updatedBody.Encode()
}

func handleMultipartBody(c *fiber.Ctx) string {
	form, err := c.MultipartForm()
	if err != nil {
		return "[multipart/form-data]"
	}

	result := url.Values{}

	for key, values := range form.Value {
		if security.IsSensitiveField(key) {
			for range values {
				result.Add(key, cn.ObfuscatedValue)
			}
		} else {
			for _, value := range values {
				result.Add(key, value)
			}
		}
	}

	for key := range form.File {
		if security.IsSensitiveField(key) {
			result.Add(key, cn.ObfuscatedValue)
		} else {
			result.Add(key, "[file]")
		}
	}

	return result.Encode()
}

// getValidBodyRequestID extracts and validates the request_id from the gRPC request body.
// Returns (id, true) when present and valid UUID; otherwise ("", false).
func getValidBodyRequestID(req any) (string, bool) {
	if r, ok := req.(interface{ GetRequestId() string }); ok {
		if rid := strings.TrimSpace(r.GetRequestId()); rid != "" && uncommons.IsUUID(rid) {
			return rid, true
		}
	}

	return "", false
}

// getMetadataID extracts a correlation id from incoming gRPC metadata if present.
func getMetadataID(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok && md != nil {
		headerID := md.Get(cn.MetadataID)
		if len(headerID) > 0 && !uncommons.IsNilOrEmpty(&headerID[0]) {
			return headerID[0]
		}
	}

	return ""
}
