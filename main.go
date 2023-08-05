package main

import (
	"net/http"
	"net/http/httputil"
	"os"
	"log"
	"net/url"
	"errors"
)


func main() {
	// default error handler in main function: if something goes wrong, abort mission
	handleErr := func(err error){
		if(err != nil){
			log.Fatal(err)
			os.exit(1)
		}
	}

	port, webhook, err := getEnvs()
	handleErr(err)

	proxy, err := mkProxy(webhook)
	handleErr(err)

	http.HandleFunc("/", mkServer(proxy))

	handleErr(http.ListenAndServe(":" + port, nil))
}

// Get the webhook url and port number from the environment
func getEnvs() (string, string, error) {
	hook := os.Getenv("DISCORD_WEBHOOK_URL")
	if hook == "" {
		return nil, nil, errors.New("no webhook url")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return hook, port, nil
}

// Make the reverse proxy which will forward the request to the discord webhook
func mkProxy(dest string) (*httputil.ReverseProxy, error) {
	if target, err := url.Parse(hook); err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		func(req *http.Request){
			req.Host = target.Host
			req.URL = target
		}(req)
	}
	return proxy, nil
}

// Make the http server that handles all incoming requests to /
func mkServer(proxy *httputil.ReverseProxy) func(w http.ResponseWriter, r *http.Request){
	return func(w http.ResponseWriter, r *http.Request){
		q := r.URL.Query()
		if !validPassword(q.Get("password")){
			return
		}
		proxy.ServeHTTP(w, r)
	}
}

// Validate a password, currently hardcoded, but should be replaced by a user token
func validPassword(password string) bool {
	return password == "supersecretpassword"
}
