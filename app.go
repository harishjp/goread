package main

/*
import (
	"log"
	"os"
	"net/http"
	"google.golang.org/appengine"
	"github.com/mjibson/goread/_third_party/github.com/gorilla/mux"
	app "github.com/mjibson/goread"
)

func main() {
	router := mux.NewRouter()
	app.RegisterHandlers(router)
	http.Handle("/", router)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
*/

import (
	"os"

	_ "github.com/harishjp/goread/appstats"
	_ "github.com/harishjp/goread/goapp"
	"google.golang.org/appengine/v2"

	"github.com/andyfusniak/stackdriver-gae-logrus-plugin"
	log "github.com/sirupsen/logrus"
)

func registerLog() {
	formatter := stackdriver.GAEStandardFormatter()
	if projectId := os.Getenv("GOOGLE_CLOUD_PROJECT"); projectId != "" {
		formatter = stackdriver.GAEStandardFormatter(stackdriver.WithProjectID(projectId))
	}
	log.SetFormatter(formatter)
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
}

func main() {
	appengine.Main()
}
