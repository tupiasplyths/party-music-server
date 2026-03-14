package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	MaxRetries  = 3
	RetryDelay  = 2 * time.Second
	MaxCacheAge = 24 * time.Hour
)

type Cache struct {
	mu          sync.RWMutex
	cacheDir    string
	ytDlpPath   string
	downloading map[string]bool
	metadata    map[string]CacheEntry
}

type CacheEntry struct {
	VideoID      string    `json:"video_id"`
	FilePath     string    `json:"file_path"`
	DownloadedAt time.Time `json:"downloaded_at"`
}

func New(cacheDir string, ytDlpPath string) *Cache {
	if cacheDir == "" {
		cacheDir = "./cache"
	}

	c := &Cache{
		cacheDir:    cacheDir,
		ytDlpPath:   ytDlpPath,
		downloading: make(map[string]bool),
		metadata:    make(map[string]CacheEntry),
	}

	c.initCacheDir()
	c.loadMetadata()

	return c
}

func (c *Cache) initCacheDir() {
	if err := os.MkdirAll(c.cacheDir, 0755); err != nil {
		log.Printf("Failed to create cache directory: %v", err)
	}
}

func (c *Cache) loadMetadata() {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(c.cacheDir, entry.Name()))
		if err != nil {
			continue
		}

		var e CacheEntry
		if err := json.Unmarshal(data, &e); err == nil && e.VideoID != "" {
			if _, err := os.Stat(e.FilePath); err == nil {
				c.metadata[e.VideoID] = e
			}
		}
	}
}

func (c *Cache) GetMetadataFilePath(videoID string) string {
	return filepath.Join(c.cacheDir, videoID+".json")
}

func (c *Cache) saveMetadata(videoID string, entry CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metadata[videoID] = entry

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("Failed to marshal cache entry: %v", err)
		return
	}

	if err := os.WriteFile(c.GetMetadataFilePath(videoID), data, 0644); err != nil {
		log.Printf("Failed to write cache metadata: %v", err)
	}
}

func (c *Cache) GetCachedFile(videoID string) (string, bool) {
	c.mu.RLock()
	entry, exists := c.metadata[videoID]
	c.mu.RUnlock()

	if !exists {
		return "", false
	}

	if _, err := os.Stat(entry.FilePath); err != nil {
		c.Remove(videoID)
		return "", false
	}

	return entry.FilePath, true
}

func (c *Cache) IsDownloading(videoID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.downloading[videoID]
}

func (c *Cache) DownloadSong(videoID string) (string, error) {
	if cachedFile, ok := c.GetCachedFile(videoID); ok {
		return cachedFile, nil
	}

	c.mu.Lock()
	if c.downloading[videoID] {
		c.mu.Unlock()
		for {
			time.Sleep(500 * time.Millisecond)
			c.mu.RLock()
			stillDownloading := c.downloading[videoID]
			c.mu.RUnlock()
			if !stillDownloading {
				break
			}
		}
		cachedFile, ok := c.GetCachedFile(videoID)
		if ok {
			return cachedFile, nil
		}
		return "", fmt.Errorf("download did not complete")
	}
	c.downloading[videoID] = true
	c.mu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= MaxRetries; attempt++ {

		filePath, err := c.downloadOnce(videoID)
		if err == nil {
			c.mu.Lock()
			c.downloading[videoID] = false
			c.mu.Unlock()
			return filePath, nil
		}

		lastErr = err

		if attempt < MaxRetries {
			time.Sleep(RetryDelay)
		}
	}

	c.mu.Lock()
	c.downloading[videoID] = false
	c.mu.Unlock()

	return "", fmt.Errorf("failed to download after %d attempts: %w", MaxRetries, lastErr)
}

func (c *Cache) downloadOnce(videoID string) (string, error) {
	audioFile := filepath.Join(c.cacheDir, videoID+".webm")

	exists, err := pathExists(audioFile)
	if err != nil {
		return "", err
	}
	if exists {
		entry := CacheEntry{
			VideoID:      videoID,
			FilePath:     audioFile,
			DownloadedAt: time.Now(),
		}
		c.saveMetadata(videoID, entry)
		return audioFile, nil
	}

	args := []string{
		"-f", "bestaudio/best",
		"-o", audioFile,
		"--no-playlist",
		"--no-warnings",
		"--socket-timeout", "10",
		"https://www.youtube.com/watch?v=" + videoID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.ytDlpPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("download timeout: %w", err)
		}
		return "", fmt.Errorf("yt-dlp error: %w (output: %s)", err, string(output))
	}

	exists, err = pathExists(audioFile)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("file not created after download")
	}

	entry := CacheEntry{
		VideoID:      videoID,
		FilePath:     audioFile,
		DownloadedAt: time.Now(),
	}
	c.saveMetadata(videoID, entry)

	return audioFile, nil
}

func (c *Cache) Remove(videoID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.metadata[videoID]
	if exists {
		os.Remove(entry.FilePath)
		os.Remove(c.GetMetadataFilePath(videoID))
		delete(c.metadata, videoID)
	}
}

func (c *Cache) CleanupOldFiles() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-MaxCacheAge)

	for videoID, entry := range c.metadata {
		if entry.DownloadedAt.Before(cutoff) {
			os.Remove(entry.FilePath)
			os.Remove(c.GetMetadataFilePath(videoID))
			delete(c.metadata, videoID)
		}
	}
}

func (c *Cache) GetCacheDir() string {
	return c.cacheDir
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
