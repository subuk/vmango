package handlers

import (
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"net/http"
)

func StaticHandler(w http.ResponseWriter, request *http.Request) {
	name := mux.Vars(request)["name"]
	log.WithField("name", name).Info("serving static file")
	http.ServeFile(w, request, "static/"+name)
}
