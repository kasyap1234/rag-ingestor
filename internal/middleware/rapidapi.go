package middleware

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

const (
	// RapidAPI sends this header with every request from their proxy
	RapidAPIProxySecretHeader = "X-RapidAPI-Proxy-Secret"
)

// RapidAPIAuth middleware validates that requests are coming from RapidAPI
// by checking the X-RapidAPI-Proxy-Secret header against the configured secret
func RapidAPIAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get the expected secret from environment variable
			expectedSecret := os.Getenv("RAPIDAPI_PROXY_SECRET")

			// If no secret is configured, allow all requests (useful for local development)
			if expectedSecret == "" {
				return next(c)
			}

			// Get the proxy secret from the request header
			proxySecret := c.Request().Header.Get(RapidAPIProxySecretHeader)

			// Validate the secret
			if proxySecret == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error":   "unauthorized",
					"message": "Missing RapidAPI proxy secret header",
				})
			}

			if proxySecret != expectedSecret {
				return c.JSON(http.StatusForbidden, map[string]string{
					"error":   "forbidden",
					"message": "Invalid RapidAPI proxy secret",
				})
			}

			return next(c)
		}
	}
}

// RapidAPIAuthWithConfig returns a RapidAPI auth middleware with custom config
func RapidAPIAuthWithConfig(config RapidAPIConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip middleware for specified paths
			for _, path := range config.SkipPaths {
				if c.Path() == path {
					return next(c)
				}
			}

			// Get the expected secret
			expectedSecret := config.ProxySecret
			if expectedSecret == "" {
				expectedSecret = os.Getenv("RAPIDAPI_PROXY_SECRET")
			}

			// If no secret is configured and AllowNoSecret is true, allow requests
			if expectedSecret == "" {
				if config.AllowNoSecret {
					return next(c)
				}
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error":   "configuration_error",
					"message": "RapidAPI proxy secret not configured",
				})
			}

			// Get the proxy secret from the request header
			proxySecret := c.Request().Header.Get(RapidAPIProxySecretHeader)

			// Validate the secret
			if proxySecret == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error":   "unauthorized",
					"message": "Missing RapidAPI proxy secret header",
				})
			}

			if proxySecret != expectedSecret {
				return c.JSON(http.StatusForbidden, map[string]string{
					"error":   "forbidden",
					"message": "Invalid RapidAPI proxy secret",
				})
			}

			return next(c)
		}
	}
}

// RapidAPIConfig holds configuration for the RapidAPI middleware
type RapidAPIConfig struct {
	// ProxySecret is the secret to validate against
	// If empty, it will be read from RAPIDAPI_PROXY_SECRET env variable
	ProxySecret string

	// SkipPaths are paths that should skip authentication
	SkipPaths []string

	// AllowNoSecret if true, allows requests when no secret is configured
	// Useful for local development
	AllowNoSecret bool
}
