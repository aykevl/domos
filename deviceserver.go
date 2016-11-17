package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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

type MQTTServer struct {
	topicPrefix      string
	deviceConnection *DeviceConnection
	client           mqtt.Client
}

func serveMQTT(address, mqttID, mqttTopicPrefix string, device *Device) {
	ms := &MQTTServer{
		topicPrefix:      mqttTopicPrefix,
		deviceConnection: device.Connect(),
	}

	opts := mqtt.NewClientOptions().AddBroker(address)
	opts.SetClientID(mqttID)
	opts.SetCleanSession(false)
	opts.SetDefaultPublishHandler(ms.publishHandler)

	go ms.deviceSendServer()

	for {
		ms.client = mqtt.NewClient(opts)
		if token := ms.client.Connect(); token.Wait() && token.Error() != nil {
			log.Println("MQTT connection failed (will wait 1min): ", token.Error())
			time.Sleep(1 * time.Minute)
			continue
		}

		topic := mqttTopicPrefix + "/+"
		if token := ms.client.Subscribe(topic, 1, nil); token.Wait() && token.Error() != nil {
			log.Fatal("Could not subscribe to topic: ", topic)
		}

		if *flagVerbose {
			log.Println("Established MQTT connection to", address)
		}

		// TODO: find a more elegant way to handle this (the library isn't very
		// helpful here).
		for ms.client.IsConnected() {
			time.Sleep(1 * time.Second)
		}
		log.Println("Closed connection with device.")
	}

	panic("unreachable")
}

func (ms *MQTTServer) publishHandler(client mqtt.Client, msg mqtt.Message) {
	if *flagVerbose {
		log.Printf("MQTT: %s: %s", msg.Topic(), string(msg.Payload()))
	}

	msgSensorName := msg.Topic()[len(ms.topicPrefix)+1:]
	msgSensorType := msgSensorName

	message := DeviceMessage{}
	err := json.Unmarshal(msg.Payload(), &message)
	if err != nil {
		log.Println("Could not read message from device:", err)
		return
	}

	// Fetch sensorId
	var sensorId int64
	var sensorType string
	err = db.QueryRow("SELECT id, type FROM sensors WHERE deviceId=? AND name=?", ms.deviceConnection.dbId, msgSensorName).Scan(&sensorId, &sensorType)
	if err == sql.ErrNoRows {
		// Sensor doesn't exist, insert it now.
		if *flagVerbose {
			log.Printf("Adding sensor %s (type %s)", msgSensorName, msgSensorType)
		}
		result, err := db.Exec("INSERT INTO sensors (deviceId, name, type) VALUES (?, ?, ?)", ms.deviceConnection.dbId, msgSensorName, msgSensorType)
		if err != nil {
			log.Println("could not add sensor:", err)
			return
		}
		sensorId, err = result.LastInsertId()
		if err != nil {
			log.Println("could not get ID of just inserted sensor:", err)
			return
		}
	} else if err != nil {
		log.Printf("could not query sensor ID for sensor '%s': %s", msgSensorName, err)
		return
	} else if msgSensorType != sensorType {
		log.Printf("could not save log row: incompatible type '%s' (expected '%s'): %#v", msgSensorType, sensorType, message)
		return
	}

	// Store sensor data
	_, err = db.Exec("INSERT INTO sensorData (sensorId, time, value, interval) VALUES (?, ?, ?, ?)", sensorId, message.TimeNs(), message.Value, message.IntervalNs())
	if err != nil {
		log.Println("could not insert sensor data:", err)
		return
	}
	ms.deviceConnection.SendLogItem(msgSensorName, message.Value, message.TimeNs(), message.IntervalNs())
}

// Write goroutine
func (ms *MQTTServer) deviceSendServer() {
	for msg := range ms.deviceConnection.SendChan {
		b, err := json.Marshal(msg)
		if err != nil {
			// must not happen
			log.Fatal("failed to marshal: ", err)
		}

		// TODO: untested
		if token := ms.client.Publish(ms.topicPrefix+"/actuator", 1, false, b); token.Wait() && token.Error() != nil {
			log.Println("Could not send message to device:", token.Error())
		}
	}
}
