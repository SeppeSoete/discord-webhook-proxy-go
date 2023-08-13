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
	"strings"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type datastoreObject struct {
	Name  string `firestore:"Name"`
	Admin bool   `firestore:"Admin"`
}

func main() {

	// default error handler in main function: if something goes wrong, abort mission
	handleErr := func(err error) {
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}
	}

	webhooks, port, err := getEnvs()
	handleErr(err)

	ctx := context.Background()
	client := mkFirestoreClient(ctx)
	defer client.Close()

	// takes url params: token, name
	http.HandleFunc("/newToken", handleNewToken(client, mkValidator(client, true)))

	// takes url params: token, name
	// token being the admin's token and name the user to delete
	http.HandleFunc("/deleteUser", handleDeleteRequest(client, mkValidator(client, true)))

	// takes url params: token, name
	// token being the admin's token and name the user to delete
	http.HandleFunc("/promoteUser", handlePromoteToAdmin(client, mkValidator(client, true)))

	// Makes endpoints for the configured hooks. These endpoints take only a token as url param and do not need admin privileges
	for webhook := range webhooks {
		proxy, err := mkProxy(webhooks[webhook])
		handleErr(err)
		http.HandleFunc("/"+webhook, mkServer(proxy, mkValidator(client, false)))
	}

	handleErr(http.ListenAndServe(":"+port, nil))
}

// Get the webhook url and port number from the environment
func getEnvs() (map[string]string, string, error) {
	hooks := os.Getenv("DISCORD_WEBHOOK_URLS")
	if hooks == "" {
		return nil, "", errors.New("no webhook url")
	}
	hookList := strings.Split(hooks, ";")
	hookmap := make(map[string]string)
	for idx := range hookList {
		vals := strings.Split(hookList[idx], "=")
		hookmap[vals[0]] = vals[1]
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return hookmap, port, nil
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

// makes the firestore client which is used to interact with the firestore db
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

// Generate a unique token for a user
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
			log.Printf("failed validating user with token: %s as admin: %t", token, admin)
			return false
		}
		if admin && !user.Admin {
			log.Printf("User %s attempted an admin request with insufficient permissions", user.Name)
			return false
		}
		return true
	}
}

// Get the user information for a token if it exists, returns an empty object if the user isn't found
func retrieveUserObjectByToken(client *firestore.Client, token string) datastoreObject {
	ctx := context.Background()
	obj, err := client.Collection("users").Doc(token).Get(ctx)
	var user datastoreObject
	if status.Code(err) == codes.NotFound {
		log.Printf("user with token: %s not found", token)
		return user
	}
	if err := obj.DataTo(&user); err != nil {
		log.Printf("failed to convert object %v to datastoreObject: %v", obj.Data(), err)

		user = datastoreObject{}
	}
	return user
}

// Deletes a user from the db, disallowing future access
func deleteUser(client *firestore.Client, name string) error {
	ctx := context.Background()
	queries, err := client.Collection("users").Where("Name", "==", name).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	log.Printf("deleting %d users with name: %s", len(queries), name)
	for i := range queries {
		queries[i].Ref.Delete(ctx)
	}
	return nil
}

// Promotes a user from the db to admin status
func promoteUser(client *firestore.Client, name string) error {
	ctx := context.Background()
	queries, err := client.Collection("users").Where("Name", "==", name).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	log.Printf("promoting %d users with name: %s", len(queries), name)
	for i := range queries {
		queries[i].Ref.Update(ctx, []firestore.Update{
			{Path: "Admin", Value: true},
		})
	}
	return nil
}

// Handler for a request to the deletion endpoint
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
			log.Println("failed to validate admin request for deletion")
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

// New token request handler
func handleNewToken(client *firestore.Client, validator func(token string) bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		name := q.Get("name")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !validator(q.Get("token")) {
			log.Println("failed to validate admin request with token ", q.Get("token"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		token := generateToken(10)
		obj := datastoreObject{name, false}
		log.Printf("making new client with name %s and token %s\n", name, token)
		ctx := context.Background()
		_, err := client.Collection("users").Doc(token).Set(ctx, obj)
		if err != nil {
			log.Println(err)
			w.WriteHeader(500)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(token))

	}
}

// Promote a user to admin
func handlePromoteToAdmin(client *firestore.Client, validator func(token string) bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		name := q.Get("name")
		token := q.Get("token")
		if name == "" || token == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !validator(token) {
			log.Println("failed to validate admin request for deletion")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		err := promoteUser(client, name)
		if err != nil {
			log.Println("could not delete user: ", err)
			w.WriteHeader(500)
			return
		}
	}
}
