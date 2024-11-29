package api

import (
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/intob/godave/types"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  types.MaxMsgLen,
	WriteBufferSize: types.MaxMsgLen,
	CheckOrigin: func(r *http.Request) bool {
		return true // Accepting all requests
	},
}

func (svc *Service) handleWebsocketConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		svc.log("ws error upgrading connection: %v", err)
		return
	}
	defer conn.Close()

	svc.log("ws client connected")

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			svc.log("ws read error:", err)
			break
		}

		svc.log("ws received: %s", string(message))

		// Echo the message back to client
		if err := conn.WriteMessage(messageType, message); err != nil {
			svc.log("ws write error:", err)
			break
		}
	}
}
