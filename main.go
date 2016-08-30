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
var flagAddress = flag.String("address", "unix:/run/domos/domos.sock", "address in the form type:address (e.g. unix:/path)")

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var db *sql.DB
var deviceSet *DeviceSet

func main() {
	flag.Parse()

	addressParts := strings.SplitN(*flagAddress, ":", 2)
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

	serverType := addressParts[0]
	serverAddress := addressParts[1]

	router := mux.NewRouter()
	router.HandleFunc("/api/ws/device", DeviceServer)
	router.HandleFunc("/api/ws/control", ControlServer)

	// open database
	var err error
	db, err = sql.Open(*flagLogType, *flagLogPath)
	if err != nil {
		log.Fatal(err)
	}

	deviceSet = NewDeviceSet()

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
