package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

const (
	// Time allowed to read the next pong message from the client.
	pongWait = 60 * time.Second

	// Send pings to client with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	writeWait = 10 * time.Second
)

func main() {
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	startHTTPServer()
}

func startHTTPServer() {
	mux := mux.NewRouter()
	mux.HandleFunc("/logs", Log).Methods("GET")

	srv := &http.Server{
		Addr: ":8080", // HTTP Default Port 80
		// Good practice to set timeouts to avoid Slowloris attacks. ie set timeouts so that a slow or malicious client doesn't hold resources forever
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      mux,
	}
	log.Println("INFO: server listening on port 8080")
	log.Fatalln(srv.ListenAndServe())
}

func Log(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Println(err)
		w.Write([]byte("Internal error"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	go writer(ws)
	reader(ws)
	defer ws.Close()

}

// Writes to client
func writer(ws *websocket.Conn) {
	pingTicker := time.NewTicker(pingPeriod)

	defer func() {
		pingTicker.Stop()
		ws.Close()
	}()

	go follow(ws, "./test.log")

	for {
		select {
		case <-pingTicker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

// Reads from client
func reader(ws *websocket.Conn) {

	defer ws.Close()
	ws.SetReadLimit(512)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			break
		}
	}
}

func follow(ws *websocket.Conn, filename string) error {
	// Maintaining array of last 10 lines
	last10 := make([]string, 0)

	// Opening the file
	file, err := os.Open(filename)
	if err != nil {
		return err
	}

	// System call
	fd, err := syscall.InotifyInit()
	if err != nil {
		return err
	}

	_, err = syscall.InotifyAddWatch(fd, filename, syscall.IN_MODIFY)
	if err != nil {
		return err
	}

	// Read the last line of file if there is a change
	r := bufio.NewReader(file)
	for {
		by, err := r.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return err
		}

		// fmt.Print(string(by))
		last10 = append(last10, string(by))
		if len(last10) == 11 {
			log.Println("check: ", last10[1:11])
			last10 = last10[1:11]
		}

		// Writing to Client
		ws.SetWriteDeadline(time.Now().Add(writeWait))
		log.Println(last10)
		if err := ws.WriteMessage(websocket.TextMessage, arrayToByte(last10)); err != nil {
			return err
		}

		if err != io.EOF {
			continue
		}

		// Wait for change at end of file
		if err = waitForChange(fd); err != nil {
			return err
		}
	}
}

func waitForChange(fd int) error {
	for {
		var buf [syscall.SizeofInotifyEvent]byte
		_, err := syscall.Read(fd, buf[:])
		if err != nil {
			return err
		}

		r := bytes.NewReader(buf[:])
		var ev = syscall.InotifyEvent{}
		err = binary.Read(r, binary.LittleEndian, &ev)

		if err != nil {
			return err
		}

		if ev.Mask&syscall.IN_MODIFY == syscall.IN_MODIFY {
			return nil
		}
	}
}

func arrayToByte(arr []string) []byte {
	var result string = ""
	for _, v := range arr {
		result = result + v
	}
	return []byte(result)
}
