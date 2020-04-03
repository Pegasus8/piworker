package auth

import (
	"time"

	jwt "github.com/dgrijalva/jwt-go"
)

// CustomClaims is the struct used to parse the claims from the JWT token
type CustomClaims struct {
	Admin bool `json:"admin"`
	jwt.StandardClaims
}

// UserInfo is the struct used to parse the auth info of some user from the
// sqlite3 database.
type UserInfo struct {
	ID               int64
	User             string
	TokenID          string
	ExpiresAt        time.Time
	LastTimeUsed     time.Time
	InsertedDatetime time.Time
}

type client struct {
	ClientAddr string
	Ticket     string
}
