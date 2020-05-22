package main

import (
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"html/template"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
)

const (
	tmplLanding     = "homepage.html"
	tmplAfterLogout = "after_logout.html"
)

var (
	HomepagePath    = "/site/homepage"
	AfterLogoutPath = "/site/after_logout"
	AssetsPath      = "/site/assets"
)

type WebServer struct {
	TemplatePaths []string
	// Frontend-related values for context
	ProviderURL string
	ClientName  string
	Theme       string
	Frontend    map[string]string
}

func (s *WebServer) Start(addr string) error {

	// Start web server
	// Load templates
	filenames := []string{}
	for _, p := range s.TemplatePaths {
		tmpls, err := listTemplates(p)
		if err != nil {
			return err
		}
		filenames = append(filenames, tmpls...)
	}

	// Add functions to context
	funcs := map[string]interface{}{
		"resolve_url_ref": func(url, ref string) string {
			return mustParseURL(url).ResolveReference(mustParseURL(ref)).String()
		},
	}

	templates, err := template.New("").Funcs(funcs).ParseFiles(filenames...)
	if err != nil {
		return err
	}

	router := mux.NewRouter()

	data := struct {
		// Frontend-related values for context
		Frontend map[string]string
		// OIDC-related settings
		ProviderURL string
		Theme       string
		ClientName  string
	}{
		Frontend:    s.Frontend,
		ProviderURL: s.ProviderURL,
		Theme:       s.Theme,
		ClientName:  s.ClientName,
	}
	router.HandleFunc(HomepagePath, siteHandler(templates.Lookup(tmplLanding), data)).Methods(http.MethodGet)
	router.HandleFunc(AfterLogoutPath, siteHandler(templates.Lookup(tmplAfterLogout), data)).Methods(http.MethodGet)

	// Assets
	router.
		PathPrefix(AssetsPath).
		Handler(
			http.StripPrefix(
				AssetsPath,
				http.FileServer(http.Dir("web/assets")),
			),
		)

	return http.ListenAndServe(addr, handlers.CORS()(router))
}

// siteHandler returns an http.HandlerFunc that serves a given template
func siteHandler(tmpl *template.Template, data interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := loggerForRequest(r)
		if err := tmpl.Execute(w, data); err != nil {
			logger.Errorf("Error executing template: %v", err)
		}
	}
}

func listTemplates(dir string) ([]string, error) {
	tmplPaths := []string{}
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".html") {
			tmplPaths = append(tmplPaths, filepath.Join(dir, f.Name()))
		}
	}
	return tmplPaths, err
}
