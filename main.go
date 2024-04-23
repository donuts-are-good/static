package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

const serVer = "v1.0.0"

var startTime time.Time
var requestTimestamps = struct {
	sync.Mutex
	timestamps []time.Time
}{}

func main() {
	helpBool := flag.Bool("help", false, "display help")
	port := flag.String("port", "3456", "port to listen on")
	staticFileDir := flag.String("directory", "./web", "directory from which static files are served")
	slidingWindowDuration := flag.Duration("statswindow", 60*time.Second, "duration for calculating request statistics")

	flag.Parse()

	if *helpBool {
		fmt.Println("Static Server " + serVer)
		fmt.Println("")
		fmt.Println("Usage:")
		fmt.Println("--help        display help")
		fmt.Println("--port        specify the port to listen on (default: " + *port + ")")
		fmt.Println("--directory   specify the directory from which static files are served (default: ./web)")
		fmt.Println("--statswindow specify the duration for calculating request statistics (default: 60 seconds)")
		fmt.Println("")
		fmt.Println("Description:")
		fmt.Println(" Static Server is an HTTP server designed to serve static files efficiently. Static Server has directory listing turned off by default.")
		fmt.Println("")
		fmt.Println("Usage Examples:")
		fmt.Println(" Run the server with default settings:")
		fmt.Println("    $ ./static-server")
		fmt.Println(" Run the server on a different port:")
		fmt.Println("    $ ./static-server --port 8080")
		fmt.Println(" Serve static files from a different directory:")
		fmt.Println("    $ ./static-server --directory /path/to/static/files")
		fmt.Println(" Change the duration for calculating request statistics:")
		fmt.Println("    $ ./static-server --statswindow 120s")
		fmt.Println("")
		fmt.Println("Endpoints:")
		fmt.Println(" - /: Serves the 'it works' page.")
		fmt.Println(" - /stats: Provides server statistics in JSON format.")
		fmt.Println(" - /favicon.ico: Serves the favicon.")
		fmt.Println(" - /static/: Serves static files from the specified static directory. Default: " + *staticFileDir)
		fmt.Println("")
		fmt.Println("Note:")
		fmt.Println(" The server listens on port " + *port + " by default.")
		return
	}

	initFolders(*staticFileDir)

	faviconPath := filepath.Join(*staticFileDir, "favicon.ico")
	if _, err := os.Stat(faviconPath); errors.Is(err, os.ErrNotExist) {
		resp, err := http.Get("https://raw.githubusercontent.com/donuts-are-good/static/master/favicon.ico")
		if err != nil {
			log.Fatalf("Error downloading favicon: %v", err)
		}
		defer resp.Body.Close()

		out, err := os.Create(faviconPath)
		if err != nil {
			log.Fatalf("Error creating favicon file: %v", err)
		}
		defer out.Close()

		_, err = io.Copy(out, resp.Body)
		if err != nil {
			log.Fatalf("Error writing favicon file: %v", err)
		}
	}

	startTime = time.Now()

	r := mux.NewRouter().StrictSlash(true)
	r.Use(loggingMiddleware)

	staticFileHandler := http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filePath := filepath.Join(*staticFileDir, r.URL.Path)
		file, err := os.Open(filePath)
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

		http.ServeFile(w, r, filePath)
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

		ramUse, threadsUse, uptimeStr, requests := stats(*slidingWindowDuration)
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

	http.ListenAndServe(":"+*port, r)
}

func initFolders(dir string) {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			log.Fatalf("Error creating directory: %v", err)
		}
	}
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

func stats(slidingWindowDuration time.Duration) (string, string, string, int) {
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
