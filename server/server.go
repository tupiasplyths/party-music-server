package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"musicbot/player"
	"musicbot/ytmusic"

	"github.com/gorilla/websocket"
)

type Server struct {
	httpServer *http.Server
	player     *player.Player
	upgrader   websocket.Upgrader
	clients    map[*websocket.Conn]bool
	clientsMu  sync.RWMutex
}

func New(addr string, p *player.Player) *Server {
	s := &Server{
		player:  p,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/queue/add", s.handleQueueAdd)
	mux.HandleFunc("/api/queue/remove", s.handleQueueRemove)
	mux.HandleFunc("/api/queue/clear", s.handleQueueClear)
	mux.HandleFunc("/api/queue/move", s.handleQueueMove)
	mux.HandleFunc("/api/player/play", s.handlePlay)
	mux.HandleFunc("/api/player/pause", s.handlePause)
	mux.HandleFunc("/api/player/resume", s.handleResume)
	mux.HandleFunc("/api/player/stop", s.handleStop)
	mux.HandleFunc("/api/player/next", s.handleNext)
	mux.HandleFunc("/api/player/prev", s.handlePrev)
	mux.HandleFunc("/api/player/volume", s.handleVolume)
	mux.HandleFunc("/api/player/state", s.handleState)
	mux.HandleFunc("/", s.handleStatic)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	p.SetBroadcast(s.Broadcast)

	return s
}

func (s *Server) broadcast(msg interface{}) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("ERROR: Failed to marshal broadcast message: %v\n", err)
		return
	}
	for conn := range s.clients {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Println("broadcast write error:", err)
		}
	}
}

func (s *Server) Broadcast(msg interface{}) {
	s.broadcast(msg)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		return nil
	})

	s.clientsMu.Lock()
	s.clients[conn] = true
	clientCount := len(s.clients)
	s.clientsMu.Unlock()

	log.Printf("DEBUG: WebSocket client connected, total clients: %d\n", clientCount)

	state := s.player.GetState()
	log.Printf("DEBUG: About to send initial state - Queue length: %d\n", len(state.Queue))
	if err := conn.WriteJSON(state); err != nil {
		log.Printf("ERROR: Failed to send initial state to WebSocket client: %v\n", err)
	}
	log.Printf("DEBUG: Finished sending initial state\n")

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, conn)
		clientCount = len(s.clients)
		s.clientsMu.Unlock()
		conn.Close()
		log.Printf("DEBUG: WebSocket client disconnected, total clients: %d\n", clientCount)
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Println("WebSocket ping error:", err)
					close(done)
					return
				}
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		default:
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}

	results, err := s.player.GetYtClient().Search(query)
	if err != nil {
		log.Printf("Search error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(results)
}

func (s *Server) handleQueueAdd(w http.ResponseWriter, r *http.Request) {
	log.Println("DEBUG: handleQueueAdd called")
	var result ytmusic.SearchResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		log.Println("DEBUG: handleQueueAdd - decode error:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("DEBUG: handleQueueAdd - Adding song: %s\n", result.Title)
	s.player.AddToQueue(result)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleQueueRemove(w http.ResponseWriter, r *http.Request) {
	indexStr := r.URL.Query().Get("index")
	if indexStr == "" {
		http.Error(w, "missing index", http.StatusBadRequest)
		return
	}

	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "invalid index", http.StatusBadRequest)
		return
	}

	s.player.RemoveFromQueue(index)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleQueueClear(w http.ResponseWriter, r *http.Request) {
	s.player.ClearQueue()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleQueueMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From int `json:"from"`
		To   int `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.player.MoveQueueItem(req.From, req.To)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	index := r.URL.Query().Get("index")
	if index != "" {
		i, err := strconv.Atoi(index)
		if err != nil {
			http.Error(w, "invalid index", http.StatusBadRequest)
			return
		}
		s.player.PlayIndex(i)
	} else {
		s.player.Play()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	s.player.Pause()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	s.player.Resume()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.player.Stop()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleNext(w http.ResponseWriter, r *http.Request) {
	s.player.Next()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePrev(w http.ResponseWriter, r *http.Request) {
	s.player.Previous()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleVolume(w http.ResponseWriter, r *http.Request) {
	vol := r.URL.Query().Get("volume")
	if vol == "" {
		http.Error(w, "missing volume", http.StatusBadRequest)
		return
	}

	v, err := strconv.Atoi(vol)
	if err != nil {
		http.Error(w, "invalid volume", http.StatusBadRequest)
		return
	}
	s.player.SetVolume(v)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(s.player.GetState())
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.Dir("web")).ServeHTTP(w, r)
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Stop() error {
	return s.httpServer.Close()
}
