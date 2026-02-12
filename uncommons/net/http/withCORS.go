package http

import (
	"github.com/LerianStudio/lib-uncommons/uncommons"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

const (
	defaultAccessControlAllowOrigin   = "*"
	defaultAccessControlAllowMethods  = "POST, GET, OPTIONS, PUT, DELETE, PATCH"
	defaultAccessControlAllowHeaders  = "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization"
	defaultAccessControlExposeHeaders = ""
)

// WithCORS is a middleware that enables CORS.
// Replace it with a real CORS middleware implementation.
func WithCORS() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins:     uncommons.GetenvOrDefault("ACCESS_CONTROL_ALLOW_ORIGIN", defaultAccessControlAllowOrigin),
		AllowMethods:     uncommons.GetenvOrDefault("ACCESS_CONTROL_ALLOW_METHODS", defaultAccessControlAllowMethods),
		AllowHeaders:     uncommons.GetenvOrDefault("ACCESS_CONTROL_ALLOW_HEADERS", defaultAccessControlAllowHeaders),
		ExposeHeaders:    uncommons.GetenvOrDefault("ACCESS_CONTROL_EXPOSE_HEADERS", defaultAccessControlExposeHeaders),
		AllowCredentials: true,
	})
}

// AllowFullOptionsWithCORS set r.Use(WithCORS) and allow every request to use OPTION method.
func AllowFullOptionsWithCORS(app *fiber.App) {
	app.Use(WithCORS())

	app.Options("/*", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
}
