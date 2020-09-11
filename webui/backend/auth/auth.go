package auth

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	// "github.com/Pegasus8/piworker/utilities/files"
	"github.com/Pegasus8/piworker/core/configs"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var signingKey []byte
var authorizedWSConns = struct {
	clients []client
	sync.RWMutex
}{}
var cfg *configs.Configs

// SetCfg sets the configs to apply on the different functions of the package.
func SetCfg(c *configs.Configs) {
	cfg = c
}

// NewJWT is a function to generate a new JWT token
func NewJWT(claim CustomClaims) (jwtToken string, err error) {
	token := jwt.New(jwt.SigningMethodHS256)
	// claims := token.Claims.(jwt.MapClaims)

	// claims["user"] = claim.User
	// claims["exp"] = claim.StandardClaims.ExpiresAt

	token.Claims = claim

	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// IsAuthorized checks if the token used is valid to access the content. In case of not required usage of tokens
// for the access to the resources, the access will be approved without making checks.
func IsAuthorized(endpoint func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		cfg.RLock()
		if !cfg.APIConfigs.RequireToken {
			cfg.RUnlock()
			endpoint(w, r)
		}
		cfg.RUnlock()

		if r.Header["Authorization"] != nil {

			// Prevents panic if an empty string is sent as token.
			if r.Header["Authorization"][0] == "" {
				log.Warn().
					Str("remoteAddr", r.RemoteAddr).
					Msg("Empty 'Authorization' header. Rejected.")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			authHeader := strings.Replace(r.Header["Authorization"][0], "Bearer ", "", 1)

			token, err := jwt.ParseWithClaims(authHeader, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("the token is using an incorrect signing method")
				}
				return signingKey, nil
			})

			if err != nil {
				if err == jwt.ErrSignatureInvalid {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				log.Error().
					Err(err).Str("remoteAddr", r.RemoteAddr).
					Msg("Error when parsing the token")
				return
			}

			if token.Valid {
				claims := token.Claims.(*CustomClaims)
				log.Info().
					Str("remoteAddr", r.RemoteAddr).
					Str("tokenOwner", claims.Subject).
					Msg("Token used")

				if r.UserAgent() != claims.UserAgent {
					log.Warn().
						Str("remoteAddr", r.RemoteAddr).
						Str("tokenOwner", claims.Subject).
						Msg("Token's UserAgent does not match with the UserAgent of the request. Rejecting request")
					return
				}

				userAuthInfo, err := ReadLastToken(claims.Subject)
				if err != nil {
					log.Error().
						Err(err).
						Str("tokenOwner", claims.Subject).
						Str("remoteAddr", r.RemoteAddr).
						Msg("Cannot check the authenticity of the token")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, "Error on the database, cannot check the authenticity of the token.")
					return
				}
				if userAuthInfo.TokenID != claims.Id {
					str := "The token used is not the same as the last one registered in the database."
					log.Warn().
						Str("tokenOwner", claims.Subject).
						Str("receivedTokenID", claims.Id).
						Str("expectedTokenID", userAuthInfo.TokenID).
						Str("remoteAddr", r.RemoteAddr).
						Msg(str)
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprintln(w, str)
					return
				}
				log.Info().
					Str("tokenOwner", claims.Subject).
					Str("remoteAddr", r.RemoteAddr).
					Msg("Token correctly checked on the database")

				err = UpdateLastTimeUsed(userAuthInfo.ID, time.Now())
				if err != nil {
					log.Panic().
						Err(err).
						Str("tokenOwner", claims.Subject).
						Str("remoteAddr", r.RemoteAddr).
						Int64("id", userAuthInfo.ID).
						Msg("Error when updating the last time used register")
				}

				endpoint(w, r)
			} else {
				log.Warn().
					Str("remoteAddr", r.RemoteAddr).
					Str("token", token.Raw).
					Msg("A client has tried to use a not valid token")
				w.WriteHeader(http.StatusUnauthorized)
			}
		} else {
			log.Warn().
				Str("remoteAddr", r.RemoteAddr).
				Msg("A client has tried to access without a token")
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
}

// NewWSTicket generates a new ticket, which will be used to authorize the WebSocket connection.
// Note: each ticket can be used only one time.
func NewWSTicket(clientAddr string) (ticket string) {
	ticket = uuid.New().String()
	c := client{
		ClientAddr: clientAddr,
		Ticket:     ticket,
	}

	authorizedWSConns.Lock()
	authorizedWSConns.clients = append(authorizedWSConns.clients, c)
	authorizedWSConns.Unlock()

	return ticket
}

// IsWSAuthorized checks if the provided ticket exists on the slice of authorized WebSocket connections.
func IsWSAuthorized(clientAddr, ticket string) bool {
	authorizedWSConns.RLock()
	for i, c := range authorizedWSConns.clients {
		if c.ClientAddr == clientAddr && c.Ticket == ticket {
			authorizedWSConns.RUnlock()

			// Remove the ticket from the slice. We don't care the order, so let's do it by the fastest way.
			authorizedWSConns.Lock()
			authorizedWSConns.clients[i] = authorizedWSConns.clients[len(authorizedWSConns.clients)-1]
			authorizedWSConns.clients = authorizedWSConns.clients[:len(authorizedWSConns.clients)-1]
			authorizedWSConns.Unlock()

			return true
		}
	}

	authorizedWSConns.RUnlock()
	return false
}

// CheckSigningKey checks if the SigningKey on the configs already exists. If not, it will be
// generated and saved on the configs file.
func CheckSigningKey() {
	cfg.RLock()

	if cfg.APIConfigs.SigningKey == "" {
		cfg.RUnlock()
		cfg.Lock()

		key := uuid.New()
		cfg.APIConfigs.SigningKey = key.String()

		cfg.Unlock()

		// Write the updated configs (with the SigningKey)
		err := cfg.Sync()
		if err != nil {
			log.Fatal().Err(err).Msg("Cannot write the signing key in the configs file")
		}

		cfg.RLock()
	}

	signingKey = []byte(cfg.APIConfigs.SigningKey)

	cfg.RUnlock()
}
