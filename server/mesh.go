package server

import (
	"net/http"
)

type ReadyMessage struct {
	UserID string `json:"userId"`
	Room   string `json:"room"`
}

func NewMeshHandler(loggerFactory LoggerFactory, wss *WSS) http.Handler {
	log := loggerFactory.GetLogger("mesh")

	fn := func(w http.ResponseWriter, r *http.Request) {
		sub, err := wss.Subscribe(w, r)
		log.Printf("mesh handler fn....")
		if err != nil {
			log.Printf("Error subscribing to websocket messages: %s", err)
		}
		for msg := range sub.Messages {
			adapter := sub.Adapter
			room := sub.Room
			clientID := sub.ClientID
			if msg.Type != "ping" {
				log.Printf("mesh client: %s", room)
				log.Printf("fn:: %s", msg.Type)
			}

			var responseEventName string
			var err error

			switch msg.Type {
			case "hangUp":
				log.Printf("[%s] hangUp event", clientID)
				adapter.SetMetadata(clientID, "")
			case "ready":
				// FIXME check for errors
				log.Printf("ready")
				payload, _ := msg.Payload.(map[string]interface{})
				adapter.SetMetadata(clientID, payload["nickname"].(string))

				clients, readyClientsErr := getReadyClients(adapter, log)
				if readyClientsErr != nil {
					log.Printf("Error retrieving clients: %s", readyClientsErr)
				}
				responseEventName = "users"
				log.Printf("Got clients: %+v", clients)
				err = adapter.Broadcast(
					NewMessage(responseEventName, room, map[string]interface{}{
						"initiator": clientID,
						"peerIds":   clientsToPeerIDs(clients),
						"nicknames": clients,
					}),
				)
			case "signal":
				log.Printf("signal")
				payload, _ := msg.Payload.(map[string]interface{})
				signal, _ := payload["signal"]
				targetClientID, _ := payload["userId"].(string)

				responseEventName = "signal"
				log.Printf("Send signal from: %s to %s", clientID, targetClientID)
				err = adapter.Emit(targetClientID, NewMessage(responseEventName, room, map[string]interface{}{
					"userId": clientID,
					"signal": signal,
				}))
			}

			if err != nil {
				log.Printf("Error sending event (event: %s, room: %s, source: %s)", responseEventName, room, clientID)
			}
		}
	}
	return http.HandlerFunc(fn)
}

func getReadyClients(adapter Adapter, l Logger) (map[string]string, error) {
	filteredClients := map[string]string{}
	l.Printf("getReadyClients, clients: %+v", filteredClients)
	clients, err := adapter.Clients()
	if err != nil {
		return filteredClients, err
	}
	for clientID, nickname := range clients {
		// if nickame hasn't been set, the peer hasn't emitted ready yet so we
		// don't connect to that peer.
		if nickname != "" {
			filteredClients[clientID] = nickname
		}
	}
	return filteredClients, nil
}

func clientsToPeerIDs(clients map[string]string) (peers []string) {
	for clientID := range clients {
		peers = append(peers, clientID)
	}
	return
}
