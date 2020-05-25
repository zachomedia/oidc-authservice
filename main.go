// Copyright © 2019 Arrikto Inc.  All Rights Reserved.

package main

import (
	"context"
	"github.com/boltdb/bolt"
	"github.com/coreos/go-oidc"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	log "github.com/sirupsen/logrus"
	"github.com/tevino/abool"
	"github.com/yosssi/boltstore/reaper"
	"github.com/yosssi/boltstore/store"
	"golang.org/x/oauth2"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Option Defaults
const (
	defaultHealthServerPort  = "8081"
	defaultServerHostname    = ""
	defaultServerPort        = "8080"
	defaultWebServerPort     = "8082"
	defaultUserIDHeader      = "kubeflow-userid"
	defaultUserIDTokenHeader = "kubeflow-userid-token"
	defaultUserIDPrefix      = ""
	defaultUserIDClaim       = "email"
	defaultSessionMaxAge     = "86400"
)

// Issue: https://github.com/gorilla/sessions/issues/200
const secureCookieKeyPair = "notNeededBecauseCookieValueIsRandom"

type server struct {
	provider               *oidc.Provider
	oauth2Config           *oauth2.Config
	store                  sessions.Store
	afterLoginRedirectURL  string
	homepageURL            string
	afterLogoutRedirectURL string
	authHeader             string
	staticDestination      string
	sessionMaxAgeSeconds   int
	userIDOpts
	caBundle []byte
}

type userIDOpts struct {
	header      string
	tokenHeader string
	prefix      string
	claim       string
}

func main() {

	// Start readiness probe immediately
	log.Infof("Starting readiness probe at %v", defaultHealthServerPort)
	isReady := abool.New()
	go func() {
		log.Fatal(http.ListenAndServe(":"+defaultHealthServerPort, http.HandlerFunc(readiness(isReady))))
	}()

	/////////////
	// Options //
	/////////////

	// OIDC Provider
	providerURL := getURLEnvOrDie("OIDC_PROVIDER")
	authURL := os.Getenv("OIDC_AUTH_URL")
	caBundlePath := os.Getenv("CA_BUNDLE")
	// OIDC Client
	oidcScopes := clean(strings.Split(getEnvOrDie("OIDC_SCOPES"), " "))
	clientID := getEnvOrDie("CLIENT_ID")
	clientSecret := getEnvOrDie("CLIENT_SECRET")
	redirectURL := getURLEnvOrDie("REDIRECT_URL")
	afterLoginRedirectURL := getEnvOrDefault("AFTER_LOGIN_URL",
		os.Getenv("STATIC_DESTINATION_URL"))
	// UserID Options
	whitelist := clean(strings.Split(os.Getenv("SKIP_AUTH_URI"), " "))
	userIDHeader := getEnvOrDefault("USERID_HEADER", defaultUserIDHeader)
	userIDTokenHeader := getEnvOrDefault("USERID_TOKEN_HEADER", defaultUserIDTokenHeader)
	userIDPrefix := getEnvOrDefault("USERID_PREFIX", defaultUserIDPrefix)
	userIDClaim := getEnvOrDefault("USERID_CLAIM", defaultUserIDClaim)
	// Server
	hostname := getEnvOrDefault("SERVER_HOSTNAME", defaultServerHostname)
	port := getEnvOrDefault("SERVER_PORT", defaultServerPort)

	// Web Server
	webServerPort := getEnvOrDefault("WEB_SERVER_PORT", defaultWebServerPort)
	webServerTemplatePaths := clean(strings.Split(os.Getenv("WEB_SERVER_TEMPLATE_PATH"), ","))
	webServerClientName := getEnvOrDefault("WEB_SERVER_CLIENT_NAME", "Kubeflow")
	webServerTemplateValues := getEnvsFromPrefix("TEMPLATE_CONTEXT_")
	webServerURLPrefix := getEnvOrDefault("WEB_SERVER_URL_PREFIX", "/authservice/")
	webServerProtectEndpoint := getBoolEnvOrDefault("WEB_SERVER_PROTECT_URL_PREFIX", true)

	homepageURL := getEnvOrDefault("HOMEPAGE_URL",
		filepath.Join(webServerURLPrefix, HomepagePath))
	afterLogoutRedirectURL := getEnvOrDefault("AFTER_LOGOUT_URL",
		filepath.Join(webServerURLPrefix, AfterLogoutPath))
	webServerThemesURL := getEnvOrDefault("WEB_SERVER_THEMES_URL", "themes")
	webServerTheme := getEnvOrDefault("WEB_SERVER_THEME", "kubeflow")

	if webServerProtectEndpoint {
		whitelist = append(whitelist, webServerURLPrefix)
	}

	// Store
	storePath := getEnvOrDie("STORE_PATH")
	// Sessions
	sessionMaxAge := getEnvOrDefault("SESSION_MAX_AGE", defaultSessionMaxAge)
	// Authentication
	authHeader := getEnvOrDefault("AUTH_HEADER", "X-Auth-Token")

	/////////////////////////////////////////////////////
	// Start server immediately for whitelisted routes //
	/////////////////////////////////////////////////////

	s := &server{}

	// Register handlers for routes
	router := mux.NewRouter()
	router.HandleFunc("/login/oidc", s.callback).Methods(http.MethodGet)
	router.HandleFunc("/logout", s.logout).Methods(http.MethodPost)

	router.PathPrefix("/").Handler(whitelistMiddleware(whitelist, isReady)(http.HandlerFunc(s.authenticate)))

	// Start server
	log.Infof("Starting server at %v:%v", hostname, port)
	stopCh := make(chan struct{})
	go func(stopCh chan struct{}) {
		log.Fatal(http.ListenAndServe(hostname+":"+port, handlers.CORS()(router)))
		close(stopCh)
	}(stopCh)

	// Start web server
	themeURL := mustParseURL(webServerThemesURL)
	themeURL.Path = filepath.Join(themeURL.Path, webServerTheme)
	webServer := WebServer{
		TemplatePaths: append([]string{"web/templates/default"}, webServerTemplatePaths...),
		ProviderURL:   providerURL.String(),
		ClientName:    webServerClientName,
		ThemeURL:      themeURL.String(),
		Frontend:      webServerTemplateValues,
	}
	log.Infof("Starting web server at %v:%v", hostname, webServerPort)
	go func() {
		log.Fatal(webServer.Start(hostname + ":" + webServerPort))
	}()

	/////////////////////////////////
	// Resume setup asynchronously //
	/////////////////////////////////

	// Read custom CA bundle
	var caBundle []byte
	var err error
	if caBundlePath != "" {
		caBundle, err = ioutil.ReadFile(caBundlePath)
		if err != nil {
			log.Fatalf("Could not read CA bundle path %s: %v", caBundlePath, err)
		}
	}

	// OIDC Discovery
	var provider *oidc.Provider
	ctx := setTLSContext(context.Background(), caBundle)
	for {
		provider, err = oidc.NewProvider(ctx, providerURL.String())
		if err == nil {
			break
		}
		log.Errorf("OIDC provider setup failed, retrying in 10 seconds: %v", err)
		time.Sleep(10 * time.Second)
	}

	endpoint := provider.Endpoint()
	if authURL != "" {
		endpoint.AuthURL = authURL
	}

	oidcScopes = append(oidcScopes, oidc.ScopeOpenID)

	// Setup Store
	// Using BoltDB by default
	db, err := bolt.Open(storePath, 0666, nil)
	if err != nil {
		log.Fatalf("Error opening bolt store: %v", err)
	}
	defer db.Close()
	// Invoke a reaper which checks and removes expired sessions periodically.
	defer reaper.Quit(reaper.Run(db, reaper.Options{}))
	store, err := store.New(db, store.Config{}, []byte(secureCookieKeyPair))
	if err != nil {
		log.Fatalf("Error creating session store: %v", err)
	}

	// Session Max-Age in seconds
	sessionMaxAgeSeconds, err := strconv.Atoi(sessionMaxAge)
	if err != nil {
		log.Fatalf("Couldn't convert session MaxAge to int: %v", err)
	}

	// Set the server values.
	// The isReady atomic variable should protect it from concurrency issues.

	*s = server{
		provider: provider,
		oauth2Config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     endpoint,
			RedirectURL:  redirectURL.String(),
			Scopes:       oidcScopes,
		},
		authHeader: authHeader,
		// TODO: Add support for Redis
		store:                  store,
		afterLoginRedirectURL:  afterLoginRedirectURL,
		homepageURL:            homepageURL,
		afterLogoutRedirectURL: afterLogoutRedirectURL,
		userIDOpts: userIDOpts{
			header:      userIDHeader,
			tokenHeader: userIDTokenHeader,
			prefix:      userIDPrefix,
			claim:       userIDClaim,
		},
		sessionMaxAgeSeconds: sessionMaxAgeSeconds,
		caBundle:             caBundle,
	}

	// Setup complete, mark server ready
	isReady.Set()

	// Block until server exits
	<-stopCh
}
