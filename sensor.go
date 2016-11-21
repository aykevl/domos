package main

import (
	"log"
	"time"
)

type Sensor struct {
	deviceId     int64
	dbId         int64
	name         string
	sensorType   string
	humanName    string
	desiredValue interface{}
}

type LogReply struct {
	Name         string        `json:"name"`
	HumanName    string        `json:"humanName"`
	DesiredValue interface{}   `json:"desiredValue"`
	Type         string        `json:"type"`
	Log          []LogReplyRow `json:"log"`
}

type LogReplyRow struct {
	Time     int64   `json:"time"`
	Interval int64   `json:"interval"`
	Value    float64 `json:"value"`
}

func GetSensor(deviceId int64, name string) *Sensor {
	sensor := &Sensor{
		deviceId: deviceId,
		name:     name,
	}
	err := db.QueryRow("SELECT id, type, humanName, desiredValue FROM sensors WHERE deviceId=? AND name=?", deviceId, name).Scan(&sensor.dbId, &sensor.sensorType, &sensor.humanName, &sensor.desiredValue)
	if err != nil {
		log.Printf("could not query sensor ID for sensor '%s': %s", name, err)
		return nil
	}
	return sensor
}

func (s *Sensor) FetchLogs(lastValueTime int64) *LogReply {
	rows, err := db.Query("SELECT time, interval, value FROM sensorData WHERE sensorId=? AND time > ? ORDER BY time", s.dbId, lastValueTime*1000*1000*1000)
	if err != nil {
		log.Print("could not fetch sensor data from log: ", err)
		return nil
	}
	defer rows.Close()

	reply := LogReply{
		Name:         s.name,
		Type:         s.sensorType,
		HumanName:    s.humanName,
		DesiredValue: s.desiredValue,
		Log:          make([]LogReplyRow, 0),
	}
	for rows.Next() {
		var logTimeNs int64
		var logIntervalNs int64
		var value float64
		err := rows.Scan(&logTimeNs, &logIntervalNs, &value)
		if err != nil {
			log.Print("could not read sensor data from log: ", err)
			return nil
		}
		logTime := logTimeNs / int64(time.Second)
		logInterval := logIntervalNs / int64(time.Second)
		reply.Log = append(reply.Log, LogReplyRow{logTime, logInterval, value})
	}

	return &reply
}
