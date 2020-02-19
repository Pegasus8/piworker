package auth

import (
	"fmt"
	"net/http"
	"time"

	// "github.com/Pegasus8/piworker/utilities/files"
	"github.com/Pegasus8/piworker/core/configs"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var signingKey []byte

// NewJWT is a function to generate a new JWT tokken
func NewJWT(claim CustomClaims) (jwtToken string, err error) {
	token := jwt.New(jwt.SigningMethodHS256)
	claims := token.Claims.(jwt.MapClaims)

	claims["user"] = claim.User
	claims["exp"] = claim.StandardClaims.ExpiresAt

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

		configs.CurrentConfigs.RLock()
		if !configs.CurrentConfigs.APIConfigs.RequireToken {
			configs.CurrentConfigs.RUnlock()
			endpoint(w, r)
		}
		configs.CurrentConfigs.RUnlock()

		if r.Header["Token"] != nil {

			// Prevents panic if an empty string is sended as token.
			if r.Header["Token"][0] == "" {
				log.Warn().
					Str("remoteAddr", r.RemoteAddr).
					Msg("The client has tried to use an empty string as token. Rejected.")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			token, err := jwt.ParseWithClaims(r.Header["Token"][0], &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("The token is using an incorrect signing method")
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
					Str("tokenOwner", claims.User).
					Msg("Token used")

				log.Printf("Token valid, checking on database...")
				userAuthInfo, err := ReadLastToken(claims.User)
				if err != nil {
					log.Error().
						Err(err).
						Str("tokenOwner", claims.User).
						Str("remoteAddr", r.RemoteAddr).
						Msg("Cannot check the authenticity of the token")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, "Error on the database, cannot check the authenticity of the token.")
					return
				}
				if userAuthInfo.Token != token.Raw {
					str := "The token used is not the same as the last one registered in the database."
					log.Warn().
						Str("tokenOwner", claims.User).
						Str("remoteAddr", r.RemoteAddr).
						Msg(str)
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprintln(w, str)
					return
				}
				log.Info().
					Str("tokenOwner", claims.User).
					Str("remoteAddr", r.RemoteAddr).
					Msg("Token correctly checked on the database")

				err = UpdateLastTimeUsed(userAuthInfo.ID, time.Now())
				if err != nil {
					log.Panic().
						Err(err).
						Str("tokenOwner", claims.User).
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

// CheckSigningKey checks if the SigningKey on the configs already exists. If not, it will be
// generated and saved on the configs file.
func CheckSigningKey() {
	// Not needed to use CurrentConfigs.RLock() because this happens only one time: when the package
	// is imported for first time.
	if configs.CurrentConfigs.APIConfigs.SigningKey == "" {
		generateSigningKey()
	}
	signingKey = []byte(configs.CurrentConfigs.APIConfigs.SigningKey)
}

func generateSigningKey() {
	key := uuid.New()
	configs.CurrentConfigs.APIConfigs.SigningKey = key.String()
	// Write the updated configs (with the SigningKey)
	err := configs.WriteToFile()
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot write the signing key in the configs file")
	}
}
