package auth

import (
	// "time"
	"net/http"
	"fmt"
	"log"

	// "github.com/Pegasus8/piworker/utilities/files"

	jwt "github.com/dgrijalva/jwt-go"
)
// TODO get the configs from the configs file 

var signingKey []byte

// CustomClaims is the struct used to parse the claims from the JWT token
type CustomClaims struct {
	User string `json:"user"`
	jwt.StandardClaims
}

func init() {
	// TODO Check if the token signing key was already setted
}

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

// IsAuthorized checks if the token used (or not) is valid to access the content
func IsAuthorized(endpoint func(http.ResponseWriter, *http.Request)) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

        if r.Header["Token"] != nil {
			
			token, err := jwt.ParseWithClaims(r.Header["Token"][0], &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
                if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, fmt.Errorf("There was an error")
                }
                return signingKey, nil
			})

            if err != nil {
				fmt.Fprintf(w, err.Error())
				panic(err.Error())
            }

            if token.Valid {
				claims := token.Claims.(*CustomClaims)
				log.Printf("Token of the user '%s' used by the IP %s\n", claims.User, r.Host)
                endpoint(w, r)
            } else {
				log.Printf("The IP %s has tried to use a not valid token: '%s'\n", r.Host, token.Raw)
			}
        } else {
			log.Printf("The IP %s has tried to access without a token\n", r.Host)
            fmt.Fprintf(w, "Not Authorized")
        }
    })
}

func generateSigningKey() {
	// TODO
}