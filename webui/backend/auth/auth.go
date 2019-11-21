package auth

import (
	"time"
	"net/http"
	"fmt"
	"log"

	// "github.com/Pegasus8/piworker/utilities/files"
	"github.com/Pegasus8/piworker/processment/configs"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/aidarkhanov/nanoid"
)
// TODO get the configs from the configs file 

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
				log.Printf("The adress %s tried to use an empty string as token. Rejected.\n", r.Host)
				return 
			}
			
			token, err := jwt.ParseWithClaims(r.Header["Token"][0], &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
                if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, fmt.Errorf("There was an error")
                }
                return signingKey, nil
			})

            if err != nil {
				log.Println("Error when parsing the token:", err.Error())
            }

            if token.Valid {
				claims := token.Claims.(*CustomClaims)
				log.Printf("Token of the user '%s' used by the IP %s\n", claims.User, r.Host)

				log.Printf("Token valid, checking on database...")
				userAuthInfo, err := ReadLastToken(claims.User)
				if err != nil {
					fmt.Fprintf(w, "Error on the database, I can't check the authenticity of the token.")
					log.Println(err.Error())
					return
				}
				if userAuthInfo.Token != token.Raw {
					str := "The token used is not the same as the last one registered in the database."
					log.Println(str)
					fmt.Fprintln(w, str)
					return
				}
				log.Println("Token correctly checked on the database")

				defer func() {
					// On case of panicking 
					if err := recover(); err != nil {
						log.Println("Recover from panic:", err)
					}
				}()
				err = UpdateLastTimeUsed(userAuthInfo.ID, time.Now())
				if err != nil {
					log.Panicln(err.Error())
				}
				
				endpoint(w, r)
            } else {
				log.Printf("The IP %s has tried to use a not valid token: '%s'\n", r.Host, token.Raw)
				fmt.Fprintf(w, "Not authorized, invalid token.")
			}
        } else {
			log.Printf("The IP %s has tried to access without a token\n", r.Host)
            fmt.Fprintf(w, "Not authorized.")
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
	configs.CurrentConfigs.APIConfigs.SigningKey = nanoid.New()
	// Write the updated configs (with the SigningKey)
	err := configs.WriteToFile()
	if err != nil {
		log.Fatal(err.Error())
	}
}
