package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type FileServer struct {
	files      map[string]time.Time
	srcDir     string
	mutex      sync.RWMutex
	fileServer http.Handler
	reqCounter *RequestCounter
}

type RequestCounter struct {
	totalRequests  int64
	requestsPerSec int64
	statusCodes    map[int]int64
	mutex          sync.Mutex
}

func NewRequestCounter() *RequestCounter {
	rc := &RequestCounter{
		statusCodes: make(map[int]int64),
	}
	go rc.startResetTimer()
	return rc
}

func (rc *RequestCounter) startResetTimer() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		rc.ResetRequestsPerSec()
	}
}

func (rc *RequestCounter) RecordRequest(statusCode int) {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()
	rc.totalRequests++
	rc.requestsPerSec++
	rc.statusCodes[statusCode]++
}

func (rc *RequestCounter) ResetRequestsPerSec() {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()
	rc.requestsPerSec = 0
}

func NewFileServer(srcDir string) *FileServer {
	fs := &FileServer{
		files:      make(map[string]time.Time),
		srcDir:     srcDir,
		fileServer: http.FileServer(http.Dir(srcDir)),
		reqCounter: NewRequestCounter(),
	}
	fs.updateFileList()
	return fs
}

func (fs *FileServer) updateFileList() {
	newFiles := make(map[string]time.Time)
	err := filepath.Walk(fs.srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			newFiles[path] = info.ModTime()
		}
		return nil
	})
	if err != nil {
		log.Printf("Error scanning directory: %v", err)
		return
	}
	fs.mutex.Lock()
	fs.files = newFiles
	fs.mutex.Unlock()
}

func (fs *FileServer) formatStatusPage() string {
	fs.reqCounter.mutex.Lock()
	defer fs.reqCounter.mutex.Unlock()

	return fmt.Sprintf(
		"Active connections: 1\n"+
			"server accepts handled requests\n"+
			" %d %d %d\n"+
			"Reading: 0 Writing: 1 Waiting: 0\n",
		fs.reqCounter.totalRequests, fs.reqCounter.totalRequests, fs.reqCounter.totalRequests,
	)
}

func (fs *FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	statusCode := 200
	fs.reqCounter.RecordRequest(statusCode)

	switch r.URL.Path {
	case "/login":
		http.ServeFile(w, r, filepath.Join(fs.srcDir, "login.html"))
	case "/get-started":
		http.ServeFile(w, r, filepath.Join(fs.srcDir, "get-started.html"))
	case "/server-status":
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, fs.formatStatusPage())
	case "/t1/dstat":
		http.ServeFile(w, r, filepath.Join(fs.srcDir, "dstat.html"))
	case "/api/stats":
		w.Header().Set("Content-Type", "application/json")
		fs.reqCounter.mutex.Lock()
		stats := map[string]interface{}{
			"requestsPerSec": fs.reqCounter.requestsPerSec,
			"totalRequests":  fs.reqCounter.totalRequests,
			"statusCodes":    fs.reqCounter.statusCodes,
		}
		fs.reqCounter.mutex.Unlock()
		json.NewEncoder(w).Encode(stats)
	default:
		fs.fileServer.ServeHTTP(w, r)
	}
}

func (fs *FileServer) StartFileWatcher() {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			fs.updateFileList()
		}
	}()
}

func (fs *FileServer) StartStatusReporter() {
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for range ticker.C {
			fs.reqCounter.mutex.Lock()
			totalRequests := fs.reqCounter.totalRequests
			requestsPerSec := fs.reqCounter.requestsPerSec
			statusCodes := fs.reqCounter.statusCodes

			fs.reqCounter.mutex.Unlock()
			log.Printf("Server Status: Total Requests: %d, Requests/Sec: %d, Status Codes: %v",
				totalRequests, requestsPerSec, statusCodes)
		}
	}()
}

func main() {
	log.SetFlags(0)
	srcDir := "src"
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		log.Printf("Creating %s directory...", srcDir)
		if err := os.MkdirAll(srcDir, 0755); err != nil {
			log.Fatalf("Failed to create directory: %v", err)
		}
	}

	fileServer := NewFileServer(srcDir)
	fileServer.StartFileWatcher()
	fileServer.StartStatusReporter()

	server := &http.Server{
		Addr:     ":79",
		Handler:  fileServer,
		ErrorLog: log.New(os.Stdout, "HTTP: ", 0),
	}

	fmt.Printf("\nALOS web server started http://localhost:79\n")
	fmt.Printf("Serving files from ./%s\n", srcDir)
	fmt.Printf("Auto-refreshing enabled (5s interval)\n")
	fmt.Printf("Server status reporting enabled (1s interval)\n\n")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
