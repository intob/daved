package api

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Accepting all requests
	},
}

func (svc *Service) handleWebsocketConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("error upgrading connection: %v", err)
		return
	}
	defer conn.Close()

	log.Println("Client Connected")

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			svc.log("/api/ws read error:", err)
			break
		}

		svc.log("/api/ws received: %s", string(message))

		// Echo the message back to client
		if err := conn.WriteMessage(messageType, message); err != nil {
			svc.log("/api/ws write error:", err)
			break
		}
	}
}
