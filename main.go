package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type datastoreObject struct {
	Name  string
	Admin bool
}

func main() {

	// default error handler in main function: if something goes wrong, abort mission
	handleErr := func(err error) {
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}
	}

	webhook, port, err := getEnvs()
	handleErr(err)

	proxy, err := mkProxy(webhook)
	handleErr(err)

	ctx := context.Background()
	client := mkFirestoreClient(ctx)
	defer client.Close()

	http.HandleFunc("/newToken", handleNewToken(client, mkValidator(client, true)))
	http.HandleFunc("/deleteUser", handleDeleteRequest(client, mkValidator(client, true)))
	http.HandleFunc("/", mkServer(proxy, mkValidator(client, false)))

	handleErr(http.ListenAndServe(":"+port, nil))
}

// Get the webhook url and port number from the environment
func getEnvs() (string, string, error) {
	hook := os.Getenv("DISCORD_WEBHOOK_URL")
	if hook == "" {
		return "", "", errors.New("no webhook url")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return hook, port, nil
}

// Make the reverse proxy which will forward the request to the discord webhook
func mkProxy(dest string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(dest)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		func(req *http.Request) {
			req.Host = target.Host
			req.URL = target
		}(req)
	}
	return proxy, nil
}

// Make the http server that handles all incoming requests to /
func mkServer(proxy *httputil.ReverseProxy, validator func(token string) bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !validator(q.Get("token")) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, r)
	}
}

func mkFirestoreClient(ctx context.Context) *firestore.Client {
	// Sets your Google Cloud Platform project ID.
	projectID := os.Getenv("PROJECT_ID")
	if projectID == "" {
		log.Fatalf("no project ID provided")
	}

	conf := &firebase.Config{ProjectID: projectID}
	app, err := firebase.NewApp(ctx, conf)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	client, err := app.Firestore(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	// Close client when done with
	// defer client.Close()
	return client
}

func generateToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// Validate a password, currently hardcoded, but should be replaced by a user token
func mkValidator(client *firestore.Client, admin bool) func(token string) bool {
	return func(token string) bool {
		user := retrieveUserObjectByToken(client, token)
		if user.Name == "" {
			return false
		}
		if admin && !user.Admin {
			return false
		}
		return true
	}
}

func retrieveUserObjectByToken(client *firestore.Client, token string) datastoreObject {
	ctx := context.Background()
	obj, err := client.Collection("users").Doc(token).Get(ctx)
	user := datastoreObject{}
	if status.Code(err) == codes.NotFound {
		return user
	}
	if err := obj.DataTo(datastoreObject{}); err != nil {
		user = datastoreObject{}
	}
	return user
}

func deleteUser(client *firestore.Client, name string) error {
	ctx := context.Background()
	queries, err := client.Collection("users").Where("name", "==", name).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for i := range queries {
		queries[i].Ref.Delete(ctx)
	}
	return nil
}

func handleDeleteRequest(client *firestore.Client, validator func(token string) bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		name := q.Get("name")
		token := q.Get("token")
		if name == "" || token == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !validator(token) {
			log.Println("failed to validate admin request")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		err := deleteUser(client, name)
		if err != nil {
			log.Println("could not delete user: ", err)
			w.WriteHeader(500)
			return
		}
	}
}

// New token request
func handleNewToken(client *firestore.Client, validator func(token string) bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !validator(q.Get("token")) {
			log.Println("failed to validate admin request")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		name := q.Get("name")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		token := generateToken(10)
		ctx := context.Background()
		obj := datastoreObject{name, false}
		log.Printf("making new client with name %s and token %s\n", name, token)
		_, err := client.Collection("users").Doc(token).Set(ctx, obj)
		if err != nil {
			log.Println(err)
			w.WriteHeader(500)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(token))

	}
}
