package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/harishjp/goread/appstats"
	"github.com/harishjp/goread/config"
	goread "github.com/harishjp/goread/goapp"
	"github.com/harishjp/goread/miniprofiler"
	_ "github.com/harishjp/goread/miniprofiler_gae"
)

func main() {
	config.LoadConfig()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	router := mux.NewRouter()

	goread.Init(router)
	appstats.Init(router)
	miniprofiler.Init(router)
	goread.InitVars()
	appstats.InitTemplates()

	http.Handle("/", router)

	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
