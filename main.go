package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

const serVer = "v1.0.0"

var slidingWindowDuration = 60 * time.Second
var startTime time.Time
var requestTimestamps = struct {
	sync.Mutex
	timestamps []time.Time
}{}

func main() {
	startTime = time.Now()

	r := mux.NewRouter().StrictSlash(true)
	r.Use(loggingMiddleware)

	staticFileDir := http.Dir("./web")
	staticFileHandler := http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, err := staticFileDir.Open(r.URL.Path)
		if err != nil {
			http.Error(w, "HTTP 404: Static Server "+serVer+" - File not found", http.StatusNotFound)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			http.Error(w, "HTTP 500: Static Server "+serVer+" - Error accessing file", http.StatusInternalServerError)
			return
		}

		if stat.IsDir() {
			http.Error(w, "HTTP 403: Static Server "+serVer+" - Directory listing is not allowed", http.StatusForbidden)
			return
		}

		http.FileServer(staticFileDir).ServeHTTP(w, r)
	}))
	r.PathPrefix("/static/").Handler(staticFileHandler)

	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "HTTP 404: Static Server "+serVer+" - That file was not found", http.StatusNotFound)
	})

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<title>Static Server %s</title>
	<style>
			body {
					font-family: monospace, sans-serif;
					display: flex;
					justify-content: center;
					align-items: center;
					height: 100vh;
					margin: 0;
			}
			p {
					text-align: center;
			}
	</style>
</head>
<body>
	<div>
			<p>Static Server %s</p>
			<p>OMG It works ;)</p>
	</div>
	<span style="position: absolute; bottom: 10px; right: 10px;">%s</span>
</body>
</html>`, serVer, serVer, serVer)
	})

	r.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ramUse, threadsUse, uptimeStr, requests := stats()
		data := map[string]interface{}{
			"Name":           "Static Server - https://github.com/donuts-are-good/static",
			"Version":        serVer,
			"Uptime":         uptimeStr,
			"Threads":        threadsUse,
			"Ram Usage":      ramUse,
			"Requests (60s)": requests,
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprint(w, string(jsonData))
	})

	r.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		http.ServeFile(w, r, "./web/favicon.ico")
	})

	http.ListenAndServe(":3456", r)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/favicon.ico" && r.URL.Path != "/" {
			log.Println(r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
		if r.URL.Path != "/favicon.ico" {
			requestTimestamps.Lock()
			requestTimestamps.timestamps = append(requestTimestamps.timestamps, time.Now())
			requestTimestamps.Unlock()
		}
	})
}

func stats() (string, string, string, int) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	ramUse := fmt.Sprintf("%v MiB", bToMb(m.Sys))

	threadsUse := fmt.Sprintf("%d/%d", runtime.GOMAXPROCS(0), runtime.NumCPU())

	uptime := time.Since(startTime)
	days := uptime / (24 * time.Hour)
	hours := (uptime % (24 * time.Hour)) / time.Hour
	minutes := (uptime % time.Hour) / time.Minute
	seconds := (uptime % time.Minute) / time.Second

	uptimeStr := fmt.Sprintf("%d days %d hours %d minutes %d seconds", days, hours, minutes, seconds)

	requestTimestamps.Lock()
	defer requestTimestamps.Unlock()
	var requests int
	cutoff := time.Now().Add(-slidingWindowDuration)

	maxAge := time.Now().Add(-2 * slidingWindowDuration)
	filteredTimestamps := []time.Time{}
	for _, ts := range requestTimestamps.timestamps {
		if ts.After(maxAge) {
			filteredTimestamps = append(filteredTimestamps, ts)
		}
	}
	requestTimestamps.timestamps = filteredTimestamps

	for _, ts := range requestTimestamps.timestamps {
		if ts.After(cutoff) {
			requests++
		}
	}

	return ramUse, threadsUse, uptimeStr, requests
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
