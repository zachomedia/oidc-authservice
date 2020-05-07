package main

import (
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"html/template"
	"net/http"
)

type StaticServer struct {
	ClientName   string
	GitlabConfig *GitlabConfig
}

type GitlabConfig struct {
	LogoutURL string
}

func (s *StaticServer) Start(addr string) error {
	// Start static page server
	// Load templates
	templates := map[string]*template.Template{
		"landing":             template.Must(template.ParseFiles("web/templates/landing.html")),
		"after_logout":        template.Must(template.ParseFiles("web/templates/after_logout.html")),
		"after_logout_gitlab": template.Must(template.ParseFiles("web/templates/after_logout_gitlab.html")),
	}
	staticRouter := mux.NewRouter()
	staticRouter.HandleFunc(defaultHomepageURL, staticPageHandler(templates["landing"], s)).Methods(http.MethodGet)
	if s.GitlabConfig != nil {
		staticRouter.HandleFunc(
			defaultAfterLogoutURL,
			staticPageHandler(templates["after_logout_gitlab"], s),
		).Methods(http.MethodGet)
	} else {
		staticRouter.HandleFunc(defaultAfterLogoutURL, staticPageHandler(templates["after_logout"], s)).Methods(http.MethodGet)
	}
	log.Infof("Starting static page server at %v", addr)
	log.Fatal(http.ListenAndServe(addr, handlers.CORS()(staticRouter)))
	return nil
}

// staticPageHandler returns an http.HandlerFunc that serves a given template
func staticPageHandler(tmpl *template.Template, data interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := loggerForRequest(r)
		if err := tmpl.Execute(w, data); err != nil {
			logger.Error("Error executing template: %v", err)
		}
	}
}
