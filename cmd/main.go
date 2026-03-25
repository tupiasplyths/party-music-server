package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"musicbot/cache"
	"musicbot/player"
	"musicbot/queue"
	"musicbot/server"
)

func main() {
	cfg, err := server.LoadConfig("config.yaml")
	if err != nil {
		cfg = &server.Config{
			Server: server.ServerConfig{
				Host: "0.0.0.0",
				Port: 8080,
			},
			Music: server.MusicConfig{
				OutputDevice: "default",
				Volume:       80,
			},
		}
	}

	q := queue.New("queue.json")
	c := cache.New(cfg.Music.CacheDir, cfg.Music.YtDlpPath)
	go func() {
		c.CleanupOldFiles()
	}()
	p := player.New(q, c, cfg.Music.Volume, cfg.Music.OutputDevice, cfg.Music.YtDlpPath, cfg.Music.FfplayPath, cfg.Music.PreloadCount)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	s := server.New(addr, p)

	go func() {
		if err := s.Start(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	go func() {
		for {
			select {
			case <-p.Context().Done():
				return
			case <-time.After(1 * time.Second):
				state := p.GetState()
				if string(state.State) == "stopped" && len(state.Queue) > 0 {
					go p.Next()
				}
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Received shutdown signal, initiating graceful shutdown...")

	shutdownTimeout := 30 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Println("Shutting down player...")
		if err := p.ShutdownWithTimeout(10 * time.Second); err != nil {
			log.Printf("Player shutdown error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		log.Println("Shutting down server...")
		if err := s.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	wg.Wait()
	log.Println("Graceful shutdown complete")
}
