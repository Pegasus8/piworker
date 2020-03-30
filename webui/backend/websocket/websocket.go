package websocket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/core/configs"
	"github.com/Pegasus8/piworker/webui/backend/auth"
	"github.com/dgrijalva/jwt-go"

	"github.com/Pegasus8/piworker/core/stats"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Read and write buffer sizes
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Upgrade func takes incoming connections and upgrade the request into a WebSocket connection
func Upgrade(w http.ResponseWriter, request *http.Request) (*websocket.Conn, error) {

	// Allow other origins connection
	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	// WebSocket connection
	ws, err := upgrader.Upgrade(w, request, nil)
	if err != nil {
		log.Error().Err(err).Str("remoteAddr", request.RemoteAddr).Msg("Error when upgrading from HTTP to WebSocker protocol")
		return ws, err
	}
	log.Info().Str("remoteAddr", request.RemoteAddr).Msg("WebSocket connection successfully established")
	// Return WebSocket connection
	return ws, nil
}

// Writer func sends data into WebSocket to the client
func Writer(conn *websocket.Conn) {
	type d struct {
		*stats.TasksStats
		*stats.RaspberryStats
	}
	type msg struct {
		Type    string `json:"type"`
		Payload d      `json:"payload"`
	}
	type payload struct {
		Token string `json:"token"`
	}
	type authMessage struct {
		Type    string  `json:"type"`
		Payload payload `json:"payload"`
	}
	var authMsg authMessage

	// The first message must be read because it contains the authentication.
	_, content, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		log.Error().
			Err(err).
			Str("remoteAddr", conn.RemoteAddr().String()).
			Msg("Cannot read the content of the WebSocket message")

		return
	}

	err = json.Unmarshal(content, &authMsg)
	if err != nil {
		conn.Close()
		log.Error().
			Err(err).
			Str("remoteAddr", conn.RemoteAddr().String()).
			Msg("Cannot parse the content of the WebSocket message on the authMessage struct")

		return
	}

	if authMsg.Type != "authentication" || len(authMsg.Payload.Token) == 0 {
		log.Warn().
			Str("remoteAddr", conn.RemoteAddr().String()).
			Msg("Token empty, closing connection")
		conn.Close()
		return
	}

	// --- Authentication ---

	log.Info().
		Str("remoteAddr", conn.RemoteAddr().String()).
		Msg("Checking token authenticity")
	token, err := jwt.ParseWithClaims(authMsg.Payload.Token, &auth.CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("The token is using an incorrect signing method")
		}
		return []byte(configs.CurrentConfigs.APIConfigs.SigningKey), nil
	})

	if err != nil {
		if err == jwt.ErrSignatureInvalid {
			// Unauthorized
			conn.Close()
			return
		}
		conn.Close()
		log.Error().
			Err(err).Str("remoteAddr", conn.RemoteAddr().String()).
			Msg("Error when parsing the token")
		return
	}

	if !token.Valid {
		log.Warn().
			Str("remoteAddr", conn.RemoteAddr().String()).
			Str("token", token.Raw).
			Msg("A client has tried to use a not valid token")
		conn.Close()

		return
	}

	claims := token.Claims.(*auth.CustomClaims)
	log.Info().
		Str("remoteAddr", conn.RemoteAddr().String()).
		Str("tokenOwner", claims.Subject).
		Msg("Token used")

	userAuthInfo, err := auth.ReadLastToken(claims.Subject)
	if err != nil {
		log.Error().
			Err(err).
			Str("tokenOwner", claims.Subject).
			Str("remoteAddr", conn.RemoteAddr().String()).
			Msg("Cannot check the authenticity of the token")
		conn.Close()

		return
	}

	if userAuthInfo.TokenID != claims.Id {
		log.Warn().
			Str("tokenOwner", claims.Subject).
			Str("receivedTokenID", claims.Id).
			Str("expectedTokenID", userAuthInfo.TokenID).
			Str("remoteAddr", conn.RemoteAddr().String()).
			Msg("The token used is not the same as the last one registered in the database")
		conn.Close()

		return
	}
	log.Info().
		Str("tokenOwner", claims.Subject).
		Str("remoteAddr", conn.RemoteAddr().String()).
		Msg("WebSocket connection authenticated")

	err = auth.UpdateLastTimeUsed(userAuthInfo.ID, time.Now())
	if err != nil {
		log.Panic().
			Err(err).
			Str("tokenOwner", claims.Subject).
			Str("remoteAddr", conn.RemoteAddr().String()).
			Int64("id", userAuthInfo.ID).
			Msg("Error when updating the last time used register")
	}

	// --- End of authentication ---

	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()

	log.Info().Str("remoteAddr", conn.RemoteAddr().String()).Msg("Sending statistics through the WebSocket")
	// Send data to client every 1 sec
	for range ticker.C {

		stats.Current.RLock()
		data := msg{
			Type: "stat",
			Payload: d{
				&stats.Current.TasksStats,
				&stats.Current.RaspberryStats,
			},
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			log.Error().Err(err).Msg("")
			stats.Current.RUnlock()
			return
		}
		stats.Current.RUnlock()

		// Send data
		err = conn.WriteMessage(websocket.TextMessage, jsonData)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Error().Err(err).Msg("")
				return
			}
			log.Warn().
				Str("remoteAddr", conn.RemoteAddr().String()).
				Msg("The client has closed the WebSocket connection")
			return
		}
	}
}
