package dashboard

import (
	"embed"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"time"
)

//go:embed templates/* static/*
var content embed.FS

type MetricEvent struct {
	Timestamp time.Time              `json:"Timestamp"`
	UserID    string                 `json:"UserID"`
	EventType string                 `json:"EventType"`
	Details   map[string]interface{} `json:"Details"`
}

func StartDashboard(metricsPath string, port int) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFS(content, "templates/dashboard.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, nil)
	})

	http.Handle("/static/", http.FileServer(http.FS(content)))

	http.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(metricsPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	go http.ListenAndServe(":"+strconv.Itoa(port), nil)
}
