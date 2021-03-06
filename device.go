package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"log"
	"sync"
	"time"
)

const GRAPH_TIME = 86400 // one day

type DeviceSet struct {
	devices map[[32]byte]*Device
	lock    sync.Mutex
}

type Device struct {
	*DeviceSet
	dbId             int64
	passwordHash     [32]byte
	nextConnectionId int
	connections      map[int]*DeviceConnection
	nextControlId    int
	controls         map[int]*ControlConnection
	actuators        map[string]interface{}
}

type DeviceConnection struct {
	id int
	*Device
	SendChan chan MessageValue
}

type ControlConnection struct {
	id int
	*Device
	sendChan chan interface{}
}

var idKey []byte

func init() {
	idKey = make([]byte, 32)
	_, err := rand.Read(idKey[:])
	if err != nil {
		panic(err)
	}
}

func idHash(idString string) [32]byte {
	idHmac := hmac.New(sha256.New, idKey)
	idHmac.Write([]byte(idString))
	macSum := idHmac.Sum(nil)
	if len(macSum) != 32 {
		panic(idHmac.Size())
	}
	var id [32]byte
	for i, b := range macSum {
		id[i] = b
	}
	return id
}

func NewDeviceSet() *DeviceSet {
	return &DeviceSet{
		devices: make(map[[32]byte]*Device),
	}
}

func (d *Device) Connect() *DeviceConnection {
	d.lock.Lock()
	defer d.lock.Unlock()

	// Now we have the device. Let's make the connection.
	connection := &DeviceConnection{
		Device:   d,
		id:       d.nextConnectionId,
		SendChan: make(chan MessageValue, 5),
	}
	d.connections[connection.id] = connection
	d.nextConnectionId++

	return connection
}

func (ds *DeviceSet) getDevice(password, name string, insert bool) *Device {
	if password == "" {
		return nil
	}

	passwordHash := idHash(password)
	var deviceId int64
	var deviceName string
	err := db.QueryRow("SELECT id,name FROM devices WHERE serial=?", password).Scan(&deviceId, &deviceName)
	if err == sql.ErrNoRows {
		if !insert {
			return nil
		}
		result, err := db.Exec("INSERT INTO devices (serial, name) VALUES (?, ?)", password, name)
		if err != nil {
			log.Println("could not add device: ", err)
			return nil
		}
		deviceId, err = result.LastInsertId()
		if err != nil {
			log.Println("could not get ID of just inserted device: ", err)
			return nil
		}
	} else if err != nil {
		log.Printf("could not query device row for '%s' (%s): %s", name, password, err)
		return nil
	}
	if deviceName != name && name != "" && insert {
		_, err := db.Exec("UPDATE devices SET name=? WHERE id=?", name, deviceId)
		if err != nil {
			log.Println("could not update device name:", err)
		}
	}

	device, ok := ds.devices[passwordHash]
	if !ok {
		device = &Device{
			dbId:         deviceId,
			passwordHash: passwordHash,
			DeviceSet:    ds,
			connections:  make(map[int]*DeviceConnection),
			controls:     make(map[int]*ControlConnection),
			actuators:    make(map[string]interface{}),
		}
		ds.devices[device.passwordHash] = device
	}
	return device
}

func (d *Device) getSensors() []*Sensor {
	rows, err := db.Query("SELECT id, name, type, humanName, desiredValue FROM sensors WHERE deviceId=?", d.dbId)
	if err != nil {
		log.Printf("could not query sensors for device %d: %s", d.dbId, err)
		return nil
	}
	defer rows.Close()
	sensors := make([]*Sensor, 0, 1)
	for rows.Next() {
		sensor := &Sensor{
			deviceId: d.dbId,
		}
		err := rows.Scan(&sensor.dbId, &sensor.name, &sensor.sensorType, &sensor.humanName, &sensor.desiredValue)
		if err != nil {
			log.Println("could not read sensor:", err)
			return nil
		}
		sensors = append(sensors, sensor)
	}
	return sensors
}

func (d *Device) mayClose() {
	if len(d.connections) == 0 && len(d.controls) == 0 {
		// No connections remaining, close Device
		delete(d.devices, d.passwordHash)
	}
}

func (d *DeviceConnection) Close() {
	d.lock.Lock()
	defer d.lock.Unlock()

	delete(d.connections, d.id)
	d.Device.mayClose()
}

func (d *Device) SendLogItem(sensorName string, value interface{}, logtime, interval time.Duration) {
	d.lock.Lock()
	defer d.lock.Unlock()

	valueFl, ok := value.(float64)
	if !ok {
		log.Println("WARNING: device sent value that is not a float")
		return
	}

	msg := ControlMessageNewLog{
		Message: "log",
		Sensor:  sensorName,
		Log: []*LogReplyRow{
			&LogReplyRow{
				Time:     int64(logtime / time.Second),
				Interval: int64(interval / time.Second),
				Value:    valueFl,
			},
		},
	}

	for _, control := range d.controls {
		control.sendChan <- msg
	}
}

func (d *DeviceConnection) SetActuator(name string, data interface{}) {
	d.lock.Lock()
	defer d.lock.Unlock()

	// Update stored actuator value
	d.actuators[name] = data

	// send message to connected controls

	msg := MessageValue{
		Message: "actuator",
		Name:    name,
		Value:   data,
	}

	for _, control := range d.controls {
		control.sendChan <- msg
	}
}

func (d *Device) AddControl(password string, sendChan chan interface{}) *ControlConnection {
	passwordHash := idHash(password)
	if subtle.ConstantTimeCompare(passwordHash[:], d.passwordHash[:]) != 1 {
		// Maybe a constant-time compare is unnecessary, but let's do it anyway
		// to be sure.
		return nil
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	control := &ControlConnection{
		Device:   d,
		id:       d.nextControlId,
		sendChan: sendChan,
	}
	d.controls[control.id] = control
	d.nextControlId++

	return control
}

func (d *ControlConnection) Close() {
	d.lock.Lock()
	defer d.lock.Unlock()

	delete(d.controls, d.id)
	d.Device.mayClose()
}

func (d *ControlConnection) SetActuator(name string, value interface{}) {
	// TODO these locks might be contended when there is a large amount of
	// messages.
	d.lock.Lock()
	defer d.lock.Unlock()

	d.actuators[name] = value

	deviceMsg := MessageValue{
		Message: "actuator",
		Name:    name,
		Value:   value,
	}
	for _, connection := range d.connections {
		connection.SendChan <- deviceMsg
	}

	controlMsg := MessageValue{
		Message: "actuator",
		Name:    name,
		Value:   value,
	}
	for _, control := range d.controls {
		if control == d {
			// what we are
			continue
		}
		control.sendChan <- controlMsg
	}
}

func (d *ControlConnection) Logs(lastValueTimes map[string]int64) map[string]*LogReply {
	d.lock.Lock()
	defer d.lock.Unlock()

	sensors := d.getSensors()
	// TODO what if deviceSensors returns nil (on error)?
	sensorReplies := make(map[string]*LogReply, len(sensors))
	now := time.Now()
	for _, sensor := range sensors {
		lastValueTime := lastValueTimes[sensor.name] // rely on the nil value
		if lastValueTime < now.Unix()-GRAPH_TIME {
			lastValueTime = now.Unix() - GRAPH_TIME
		}
		sensorReplies[sensor.name] = sensor.FetchLogs(lastValueTime)
	}
	return sensorReplies
}
