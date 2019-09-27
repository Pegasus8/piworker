package auth

import (
	"time"
	jwt "github.com/dgrijalva/jwt-go"
)

// CustomClaims is the struct used to parse the claims from the JWT token
type CustomClaims struct {
	User string `json:"user"`
	jwt.StandardClaims
}

// UserInfo is the struct used to parse the auth info of some user from the 
// sqlite3 database.
type UserInfo struct {
	ID string
	User string
	Token string
	ExpiresAt time.Time
	LastTimeUsed time.Time
	InsertedDatetime time.Time
}