package gateway

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"paracetamol/internal/config"
	"paracetamol/internal/identity"
)

type bearerAuth struct {
	enabled bool
	digest  [sha256.Size]byte
}

func newBearerAuth(key string) (bearerAuth, error) {
	if key == "" {
		return bearerAuth{}, nil
	}
	if err := config.ValidateGatewayKey(key); err != nil {
		return bearerAuth{}, err
	}
	return bearerAuth{enabled: true, digest: sha256.Sum256([]byte(key))}, nil
}

func (auth bearerAuth) allow(writer http.ResponseWriter, request *http.Request) bool {
	if !auth.enabled {
		return true
	}
	values := request.Header.Values("Authorization")
	if len(values) == 1 {
		fields := strings.Fields(values[0])
		if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
			digest := sha256.Sum256([]byte(fields[1]))
			if subtle.ConstantTimeCompare(auth.digest[:], digest[:]) == 1 {
				return true
			}
		}
	}
	writer.Header().Set("WWW-Authenticate", `Bearer realm="`+identity.CommandName+`"`)
	writer.Header().Set("Cache-Control", "no-store")
	writeAPIError(writer, http.StatusUnauthorized, "authentication_error", "invalid_api_key", "A valid gateway Bearer token is required")
	return false
}
