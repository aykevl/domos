package main

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type DeviceMessage struct {
	Message  string      `json:"message"`  // message type: 'connect', 'sensorLog'
	Name     string      `json:"name"`     // device human name / sensor name / actuator name
	Serial   string      `json:"serial"`   // device serial number (id)
	Time     int64       `json:"time"`     // log time
	Type     string      `json:"type"`     // log type
	Interval int64       `json:"interval"` // log interval
	Value    interface{} `json:"value"`    // log value, actuator data type
}

func (m DeviceMessage) Integer() int64 {
	switch v := m.Value.(type) {
	case int64:
		return v
	default:
		return 0
	}
}

func (m DeviceMessage) Float() float64 {
	switch v := m.Value.(type) {
	case float64:
		return v
	default:
		return 0
	}
}

func (m DeviceMessage) TimeNs() time.Duration {
	return time.Duration(m.Time) * time.Second
}

func (m DeviceMessage) IntervalNs() time.Duration {
	return time.Duration(m.Interval) * time.Second
}

func DeviceServer(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Could not upgrade device WebSocket: ", err)
		return
	}
	defer conn.Close()
	log.Println("New device")

	conn.WriteJSON(MessageTimestamp{
		Message:   "time",
		Timestamp: time.Now().Unix(),
	})

	exitChan := make(chan struct{})

	var deviceConnection *DeviceConnection

	// Read goroutine
	for {
		message := DeviceMessage{}
		err := conn.ReadJSON(&message)
		if err != nil {
			if err != io.EOF {
				log.Println("Could not read message from device: ", err)
			}
			break
		}

		if message.Message == "connect" {
			if deviceConnection != nil {
				// bogus message?
				log.Println("WARNING: got connect message more than once")
				continue
			}
			deviceConnection = deviceSet.Connect(message.Serial, message.Name)
			if deviceConnection == nil {
				log.Println("WARNING: could not connect (invalid connect message)")
				continue
			}
			go deviceSendServer(conn, deviceConnection, exitChan)
			continue
		}

		// Only proceed if connected
		if deviceConnection == nil {
			log.Println("Not properly connected, but sending message:", message.Message)
			continue
		}

		// Some other message
		if message.Message == "sensorLog" {
			// Fetch sensorId
			var sensorId int64
			var sensorType string
			err = db.QueryRow("SELECT id, type FROM sensors WHERE deviceId=? AND name=?", deviceConnection.dbId, message.Name).Scan(&sensorId, &sensorType)
			if err == sql.ErrNoRows {
				// Sensor doesn't exist, insert it now.
				result, err := db.Exec("INSERT INTO sensors (name, type) VALUES (?, ?)", message.Name, message.Type)
				if err != nil {
					log.Println("could not add sensor: ", err)
					continue
				}
				sensorId, err = result.LastInsertId()
				if err != nil {
					log.Println("could not get ID of just inserted sensor: ", err)
					continue
				}
			} else if err != nil {
				log.Printf("could not query sensor ID for sensor '%s': %s", message.Name, err)
				continue
			} else if message.Type != sensorType {
				log.Printf("could not save log row: incompatible type '%s' (expected '%s'): %#v", message.Type, sensorType, message)
				continue
			}

			// Store sensor data
			_, err = db.Exec("INSERT INTO sensorData (sensorId, time, value, interval) VALUES (?, ?, ?, ?)", sensorId, message.TimeNs(), message.Value, message.IntervalNs())
			if err != nil {
				log.Println("could not insert sensor data: ", err)
				continue
			}
			deviceConnection.SendLogItem(message.Name, message.Value, message.TimeNs(), message.IntervalNs())

		} else if message.Message == "actuator" {
			// send this immediately to the control
			deviceConnection.SetActuator(message.Name, message.Value)

		} else {
			log.Println("WARNING: unknown message: ", message.Message)
		}
	}

	// properly exit the sending (writing) goroutine
	exitChan <- struct{}{}
	<-exitChan // wait on exit

	log.Println("Closed connection with device")
}

// Write goroutine
func deviceSendServer(conn *websocket.Conn, dc *DeviceConnection, exitChan chan struct{}) {
	for {
		select {
		case msg := <-dc.SendChan:
			err := conn.WriteJSON(msg)
			if err != nil {
				log.Println("Could not send message to device: ", err)
				dc.Close()
				break
			}
		case <-exitChan:
			// signal we've received the signal
			// FIXME: there's a race condition: messages may still be sent
			// on SendChan.
			dc.Close()
			exitChan <- struct{}{}
			break
		}
	}
}
