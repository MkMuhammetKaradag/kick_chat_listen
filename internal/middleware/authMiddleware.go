package middleware

import (
	"kick-chat/domain"
	"strings"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

func respondWithError(c *fiber.Ctx, code int, message string) error {
	c.Set("Content-Type", "application/json")
	return c.Status(code).JSON(fiber.Map{"error": message})
}

type AuthMiddleware struct {
	sessionManager SessionManagerType
}

func NewAuthMiddleware(redisRepo SessionManagerType) *AuthMiddleware {
	return &AuthMiddleware{sessionManager: redisRepo}
}

func (m *AuthMiddleware) Authenticate() fiber.Handler {
	return func(c *fiber.Ctx) error {

		var token string

		if strings.Contains(c.Get("Connection"), "Upgrade") && c.Get("Upgrade") == "websocket" {

			token = c.Query("token")
			if token == "" {
				token = c.Get("session_token")
			}
		} else {
			cookieSessionId := c.Cookies("session_token")
			if cookieSessionId == "" {
				return respondWithError(c, fiber.StatusUnauthorized, "Unauthorized: missing session")
			}
			token = cookieSessionId
		}
		if !m.sessionManager.IsValid(c.UserContext(), token) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid or expired token"})
		}
		userData, err := m.sessionManager.GetSession(c.UserContext(), token)
		if err != nil {
			return respondWithError(c, fiber.StatusUnauthorized, "missing session")
		}

		c.Locals("userData", userData)

		return c.Next()
	}
}

func GetUserData(c *fiber.Ctx) (*domain.Session, bool) {
	userData, ok := c.Locals("userData").(*domain.Session)
	return userData, ok
}
func GetUserDataFromWS(conn *websocket.Conn) (map[string]string, bool) {
	val := conn.Locals("userData")
	userData, ok := val.(map[string]string)
	return userData, ok
}
