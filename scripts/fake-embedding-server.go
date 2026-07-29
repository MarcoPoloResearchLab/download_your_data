package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 18999, "loopback port")
	flag.Parse()
	http.HandleFunc("/health", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	http.HandleFunc("/v1/embeddings", func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload struct {
			Input []string `json:"input"`
		}
		if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
			http.Error(responseWriter, decodeError.Error(), http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(payload.Input))
		for inputIndex := range payload.Input {
			data[inputIndex] = map[string]any{
				"index":     inputIndex,
				"embedding": []float64{1, 0, 0},
			}
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(responseWriter).Encode(map[string]any{"data": data})
	})
	log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), nil))
}
