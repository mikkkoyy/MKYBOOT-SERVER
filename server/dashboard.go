package main

import (
	"embed"
	"net/http"
)

//go:embed dashboard.html
var dashboardFS embed.FS

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := dashboardFS.ReadFile("dashboard.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "dashboard missing")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}