package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

var flagLogType = flag.String("logtype", "sqlite3", "database type for logfile")
var flagLogPath = flag.String("log", "", "log address")
var flagServer = flag.String("server", "unix:/run/domos/domos.sock", "server address in the form type:address (e.g. unix:/path)")
var flagMQTT = flag.String("mqtt", "tcp://localhost:1883", "MQTT URL")
var flagMQTTID = flag.String("mqtt-id", "domo-server", "MQTT client ID")
var flagMQTTTopicPrefix = flag.String("mqtt-topic-prefix", "", "MQTT topic prefix (e.g. /user/location)")
var flagSerial = flag.String("serial", "", "serial (key) of the device")
var flagVerbose = flag.Bool("verbose", false, "verbose logging")

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var db *sql.DB

func main() {
	flag.Parse()

	addressParts := strings.SplitN(*flagServer, ":", 2)
	if *flagLogPath == "" {
		fmt.Fprintln(os.Stderr, "Empty log path argument.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if len(addressParts) != 2 || addressParts[0] == "" || addressParts[1] == "" {
		fmt.Fprintln(os.Stderr, "Invalid address argument.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if len(*flagSerial) == 0 {
		fmt.Fprintln(os.Stderr, "No serial for the device.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if len(*flagMQTTTopicPrefix) == 0 {
		fmt.Fprintln(os.Stderr, "No MQTT topic prefix.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// open database
	var err error
	db, err = sql.Open(*flagLogType, *flagLogPath)
	if err != nil {
		log.Fatal(err)
	}

	device := NewDeviceSet().getDevice(*flagSerial, "", true)
	if device == nil {
		log.Fatal("Could not load device, exiting.")
	}

	serverType := addressParts[0]
	serverAddress := addressParts[1]

	router := mux.NewRouter()
	router.HandleFunc("/api/ws/control", func(w http.ResponseWriter, r *http.Request) {
		ControlServer(w, r, device)
	})

	go serveMQTT(*flagMQTT, *flagMQTTID, *flagMQTTTopicPrefix, device)

	if serverType == "unix" {
		err := os.Remove(serverAddress)
		if err != nil && !os.IsNotExist(err) {
			log.Fatal("could not remove old socket file: ", err)
		}
	}

	listener, err := net.Listen(serverType, serverAddress)
	if err != nil {
		log.Fatal("could not listen on socket: ", listener)
	}

	if serverType == "unix" {
		err := os.Chmod(serverAddress, 0660)
		if err != nil {
			log.Fatal("could not chmod server socket: ", err)
		}
	}

	err = http.Serve(listener, router)
	if err != nil {
		log.Fatal("error while serving: ", err)
	}
}
