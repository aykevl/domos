package main

import (
	"io"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// Received message from control
type ControlMessage struct {
	Message      string                 `json:"message"`      // 'connect', 'actuator'
	Name         string                 `json:"name"`         // actuator name
	Password     string                 `json:"password"`     // client password (no username)
	LastLogTimes map[string]LastLogTime `json:"lastLogTimes"` // last timestamp of a sensor log
	Value        interface{}            `json:"value"`        // actuator
}
type LastLogTime struct {
	LastLogTime int64 `json:"lastTime"`
}

type ControlMessageConnected struct {
	Message   string                 `json:"message"`
	Logs      map[string]*LogReply   `json:"logs"`
	Actuators map[string]interface{} `json:"actuators"`
}

type ControlMessageError struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

type ControlMessageNewLog struct {
	Message string         `json:"message"`
	Sensor  string         `json:"sensor"`
	Log     []*LogReplyRow `json:"log"`
}

func ControlServer(w http.ResponseWriter, r *http.Request, device *Device) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Could not upgrade control WebSocket: ", err)
		return
	}
	defer conn.Close()

	recv := make(chan ControlMessage)
	send := make(chan interface{})
	defer close(recv)

	go runControlServer(recv, send, device)

	go func() {
		for msg := range send {
			err := conn.WriteJSON(msg)
			if err == websocket.ErrCloseSent {
				// TODO: log warning message that is not logged by default
				return
			}
			if err != nil {
				log.Println("Could not send message: ", err)
				continue
			}
		}
	}()

	for {
		msg := ControlMessage{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			if err != io.EOF {
				log.Println("Could not read message from control: ", err)
			}
			break
		}
		recv <- msg
	}
}

func runControlServer(recv chan ControlMessage, send chan interface{}, device *Device) {
	msg := <-recv
	defer close(send)

	if msg.Message != "connect" {
		send <- ControlMessageError{
			Message: "disconnected",
			Error:   "expected first message to be connect message",
		}
		return
	}

	controlConnection := device.AddControl(msg.Password, send)
	if controlConnection == nil {
		// password invalid
		send <- ControlMessageError{
			Message: "disconnected",
			Error:   "connection refused - invalid password?",
		}
		return
	}
	defer controlConnection.Close()

	lastValueTimes := make(map[string]int64, len(msg.LastLogTimes))
	for n, subscr := range msg.LastLogTimes {
		lastValueTimes[n] = subscr.LastLogTime
	}
	send <- ControlMessageConnected{
		Message:   "connected",
		Logs:      controlConnection.Logs(lastValueTimes),
		Actuators: controlConnection.actuators,
	}

	for msg := range recv {
		switch msg.Message {
		case "actuator":
			if msg.Value == nil {
				log.Println("Control sent empty actuator data")
				continue
			}
			controlConnection.SetActuator(msg.Name, msg.Value)
		default:
			log.Println("Unknown control message:", msg.Message)
		}
	}
}
