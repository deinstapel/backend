package qbinHTTP

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/qbin-io/backend"
	"github.com/urfave/negroni"
	"golang.org/x/crypto/acme/autocert"
)

type Configuration struct {
	ListenHTTP    string
	ListenHTTPS   string
	FrontendPath  string
	Root          string
	path          string
	domain        string
	CertWhitelist []string
	ForceRoot     bool
	Hsts          string
}

var config Configuration

// initializeConfig will normalize the options and create the "config" object.
func initializeConfig(initialConfig Configuration) {
	config = initialConfig
	// Transform frontendPath to an absolute path
	frontendPath, err := filepath.Abs(config.FrontendPath)
	if err != nil {
		qbin.Log.Critical("Frontend path couldn't be resolved.")
		panic(err)
	}
	config.FrontendPath = frontendPath

	config.Root = strings.TrimSuffix(config.Root, "/")

	// Extract "path" fron "root"
	rootParts := strings.SplitAfterN(config.Root, "/", 4) // https://example.org/[grab this part]
	config.path = ""
	if len(rootParts) > 3 { // Otherwise: application in root folder
		config.path = rootParts[3]
	}
	config.path = strings.TrimSuffix("/"+strings.TrimPrefix(config.path, "/"), "/")

	rootParts = strings.SplitN(strings.ToLower(config.Root), "://", 2)
	config.domain = strings.Split(rootParts[len(rootParts)-1], "/")[0]
}

// StartHTTP launches the HTTP server which is responsible for the frontend and the HTTP API.
func StartHTTP(initialConfig Configuration) {
	// Configure
	qbin.Log.Debug("Initializing HTTP server...")
	initializeConfig(initialConfig)

	// Route
	qbin.Log.Debug("Setting up routes...")
	r := mux.NewRouter()
	setupRoutes(r)

	// Middlewares
	n := negroni.New(negroni.NewRecovery())
	// Add important headers
	n.UseHandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Server", "qbin")
		res.Header().Add("X-Content-Type", "nosniff") // We're not a CDN.
		if config.Hsts != "" {
			res.Header().Add("Strict-Transport-Security", config.Hsts)
		}
	})
	// Redirect to root
	if config.ForceRoot {
		n.UseFunc(func(res http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
			if req.Host != config.domain || !strings.HasPrefix(req.URL.Path, config.path+"/") {
				if !strings.HasPrefix(req.URL.Path, config.path+"/") {
					res.Header().Add("Location", config.Root)
					res.WriteHeader(301)
				} else {
					res.Header().Add("Location", config.Root+config.path+strings.TrimPrefix(req.URL.Path, config.path))
					res.WriteHeader(301)
				}
			} else {
				next(res, req)
			}
		})
	}
	n.UseHandler(r)

	// Serve
	if config.ListenHTTPS != "none" {
		qbin.Log.Noticef("HTTPS server starting on %s for redirection", config.ListenHTTP)
		go listenHTTP(redirector{})
		if config.ListenHTTP != "none" {
			qbin.Log.Noticef("HTTPS server starting on %s, you should be able to reach it at %s", config.ListenHTTPS, config.Root)
			go listenHTTPS(n)
		}
	} else {
		qbin.Log.Noticef("HTTP server starting on %s, you should be able to reach it at %s", config.ListenHTTP, config.Root)
		go listenHTTP(n)
	}
}

func listenHTTPS(r http.Handler) {
	whitelist := make(map[string]bool, len(config.CertWhitelist))
	for _, h := range config.CertWhitelist {
		whitelist[h] = true
	}

	certManager := autocert.Manager{
		Prompt: autocert.AcceptTOS,
		HostPolicy: func(_ context.Context, host string) error {
			if host != config.domain && whitelist[host] != true {
				return errors.New("TLS host not configured: " + host)
			}
			return nil
		},
		Cache: autocert.DirCache("certs"),
	}
	server := &http.Server{
		Addr:    config.ListenHTTPS,
		Handler: r,
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
		},
	}

	err := server.ListenAndServeTLS("", "")
	if err != nil {
		qbin.Log.Errorf("HTTPS server error: %s", err)
		panic(err)
	}
}

func listenHTTP(r http.Handler) {
	err := http.ListenAndServe(config.ListenHTTP, r)
	if err != nil {
		qbin.Log.Errorf("HTTP server error: %s", err)
		panic(err)
	}
}

type redirector struct{}

func (redirector) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	res.Header().Add("Server", "qbin")
	res.Header().Add("Location", config.Root+req.URL.EscapedPath())
	res.WriteHeader(301)
	res.Write([]byte(""))
}
