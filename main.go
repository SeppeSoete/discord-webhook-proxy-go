package main
import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", getRoot)
	http.HandleFunc("/trigger", trigger)
	err := http.ListenAndServe(":3000", nil)
	if err != nil {
		fmt.Println("something went wrong")
	}
}

func getRoot(w http.ResponseWriter, r *http.Request) {
	fmt.Println("got /");
	http.Get("http://localhost:3000/trigger")
}

func trigger(w http.ResponseWriter, r *http.Request) {
	fmt.Println("triggered")
}
