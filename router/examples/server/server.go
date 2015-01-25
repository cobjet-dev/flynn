package main

import (
	"log"
	"net/http"
	"os"

	"github.com/flynn/flynn/discoverd/client"
)

func main() {
	addr := ":" + os.Args[1]
	if _, err := discoverd.AddServiceAndRegister("example-server", addr); err != nil {
		log.Fatal(err)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("Listening on " + os.Args[1]))
	})
	log.Fatal(http.ListenAndServe(addr, nil))
}
