package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	userIDKey   contextKey = "userID"
	userRoleKey contextKey = "userRole"
)

func isSafeUserID(s string) bool {
	if s == "" || len(s) > 128 || strings.Contains(s, "..") || strings.ContainsAny(s, `/\`) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7F || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			return false
		}
	}
	return true
}

func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.TrimSpace(secret) == "" {
				writeAuthError(w, "Authentication not configured")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeAuthError(w, "Authorization header required")
				return
			}

			bearerToken := strings.SplitN(authHeader, " ", 2)
			if len(bearerToken) != 2 || bearerToken[0] != "Bearer" || bearerToken[1] == "" {
				writeAuthError(w, "Invalid authorization header format")
				return
			}

			token, err := jwt.Parse(
				bearerToken[1],
				func(token *jwt.Token) (any, error) {
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					return []byte(secret), nil
				},
				jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
				jwt.WithExpirationRequired(),
			)

			if err != nil || !token.Valid {
				writeAuthError(w, "Invalid or expired token")
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeAuthError(w, "Invalid token claims")
				return
			}

			userID, ok := claims["sub"].(string)
			userID = strings.TrimSpace(userID)
			if !ok || !isSafeUserID(userID) {
				writeAuthError(w, "Invalid user ID in token")
				return
			}

			role := "user"
			if rawRole, ok := claims["role"].(string); ok && rawRole == "admin" {
				role = "admin"
			}

			ctx := WithUser(r.Context(), userID, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(userIDKey).(string); ok {
		return userID
	}
	return ""
}

func GetUserRole(ctx context.Context) string {
	if role, ok := ctx.Value(userRoleKey).(string); ok {
		return role
	}
	return ""
}

func WithUser(ctx context.Context, userID, role string) context.Context {
	ctx = context.WithValue(ctx, userIDKey, userID)
	return context.WithValue(ctx, userRoleKey, role)
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
