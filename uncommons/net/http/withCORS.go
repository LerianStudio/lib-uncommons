package http

import (
	"log"
	"strconv"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

const (
	defaultAccessControlAllowOrigin   = "*"
	defaultAccessControlAllowMethods  = "POST, GET, OPTIONS, PUT, DELETE, PATCH"
	defaultAccessControlAllowHeaders  = "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization"
	defaultAccessControlExposeHeaders = ""
	defaultAllowCredentials           = false
)

// WithCORS is a middleware that enables CORS.
// Replace it with a real CORS middleware implementation.
func WithCORS() fiber.Handler {
	allowCredentials := defaultAllowCredentials

	if parsed, err := strconv.ParseBool(uncommons.GetenvOrDefault("ACCESS_CONTROL_ALLOW_CREDENTIALS", "false")); err == nil {
		allowCredentials = parsed
	}

	origins := uncommons.GetenvOrDefault("ACCESS_CONTROL_ALLOW_ORIGIN", defaultAccessControlAllowOrigin)

	if origins == "*" || origins == "" {
		log.Println("[WARN] CORS: AllowOrigins is set to wildcard (*). Consider restricting to specific origins in production.")
	}

	if origins == "*" && allowCredentials {
		log.Println("[WARN] CORS: AllowOrigins=* with AllowCredentials=true is rejected by browsers per the CORS spec. Configure specific origins.")
	}

	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     uncommons.GetenvOrDefault("ACCESS_CONTROL_ALLOW_METHODS", defaultAccessControlAllowMethods),
		AllowHeaders:     uncommons.GetenvOrDefault("ACCESS_CONTROL_ALLOW_HEADERS", defaultAccessControlAllowHeaders),
		ExposeHeaders:    uncommons.GetenvOrDefault("ACCESS_CONTROL_EXPOSE_HEADERS", defaultAccessControlExposeHeaders),
		AllowCredentials: allowCredentials,
	})
}

// AllowFullOptionsWithCORS set r.Use(WithCORS) and allow every request to use OPTION method.
func AllowFullOptionsWithCORS(app *fiber.App) {
	if app == nil {
		return
	}

	app.Use(WithCORS())

	app.Options("/*", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
}
