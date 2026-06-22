package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dynoconf/dynoconf/internal/events"
)

// handleSSE streams live events to the UI so variable tables and the active
// connection counters update in real time. The client filters by service.
//
// Authenticated users receive events for every service; the UI only renders
// updates for services it is currently showing (and which the user can see).
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe to all services (serviceID 0).
	ch, unsubscribe := s.broker.Subscribe(0)
	defer unsubscribe()

	// Initial comment to open the stream.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			payload, _ := json.Marshal(ev)
			eventName := "message"
			switch ev.Kind {
			case events.KindVar:
				eventName = "variable"
			case events.KindConns:
				eventName = "connections"
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, payload)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
