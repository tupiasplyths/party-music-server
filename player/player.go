package player

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"

	"musicbot/cache"
	"musicbot/queue"
	"musicbot/ytmusic"
)

type Player struct {
	mu           sync.RWMutex
	queue        *queue.Queue
	cache        *cache.Cache
	ytClient     *ytmusic.Client
	state        PlaybackState
	volume       int
	device       string
	currentItem  *queue.QueueItem
	currentCmd   *exec.Cmd
	broadcast    func(interface{})
	ctx          context.Context
	cancel       context.CancelFunc
	ytDlpPath    string
	ffplayPath   string
	preloadCount int
}

type PlaybackState string

const (
	StateStopped PlaybackState = "stopped"
	StatePlaying PlaybackState = "playing"
	StatePaused  PlaybackState = "paused"
)

func New(q *queue.Queue, c *cache.Cache, volume int, device string, ytDlpPath, ffplayPath string, preloadCount int) *Player {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Player{
		queue:        q,
		cache:        c,
		volume:       volume,
		device:       device,
		state:        StateStopped,
		ctx:          ctx,
		cancel:       cancel,
		ytDlpPath:    ytDlpPath,
		ffplayPath:   ffplayPath,
		preloadCount: preloadCount,
	}
	p.ytClient = ytmusic.New(ytDlpPath)

	return p
}

func (p *Player) SetBroadcast(fn func(interface{})) {
	p.broadcast = fn
}

func (p *Player) broadcastState() {
	if p.broadcast != nil {
		state := p.GetState()
		p.broadcast(state)
	}
}

func (p *Player) GetState() PlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.getStateLocked("")
}

func (p *Player) GetStateForClient(clientIP string) PlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.getStateLocked(clientIP)
}

func (p *Player) getStateLocked(clientIP string) PlayerState {
	queueItems := p.queue.GetAll()
	currentIndex := p.queue.GetCurrentIndex()

	state := PlayerState{
		State:            p.state,
		Volume:           p.volume,
		CurrentIndex:     currentIndex,
		QueueLimit:       queue.MaxSongsPerClient,
		ClientQueueCount: 0,
	}

	if clientIP != "" {
		state.ClientQueueCount = p.queue.CountClientSongs(clientIP, true)
	}

	if p.currentItem != nil {
		state.Current = &NowPlaying{
			Title:     p.currentItem.Title,
			Artist:    p.currentItem.Artist,
			Thumbnail: p.currentItem.Thumbnail,
			Duration:  p.currentItem.Duration,
		}
	} else {
		queueCurrent := p.queue.GetCurrent()
		if queueCurrent != nil {
			state.Current = &NowPlaying{
				Title:     queueCurrent.Title,
				Artist:    queueCurrent.Artist,
				Thumbnail: queueCurrent.Thumbnail,
				Duration:  queueCurrent.Duration,
			}
		}
	}

	state.Queue = queueItems
	state.CurrentIndex = currentIndex

	return state
}

type PlayerState struct {
	State            PlaybackState     `json:"state"`
	Volume           int               `json:"volume"`
	Current          *NowPlaying       `json:"current,omitempty"`
	Queue            []queue.QueueItem `json:"queue"`
	CurrentIndex     int               `json:"current_index"`
	QueueLimit       int               `json:"queue_limit"`
	ClientQueueCount int               `json:"client_queue_count"`
}

type NowPlaying struct {
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Thumbnail string `json:"thumbnail"`
	Duration  int    `json:"duration"`
}

func (p *Player) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playLocked()
}

func (p *Player) playLocked() error {
	if p.state == StatePlaying {
		return nil
	}

	item := p.queue.GetCurrent()
	if item == nil {
		item = p.queue.Next()
	}
	if item == nil {
		return fmt.Errorf("queue is empty")
	}

	p.currentItem = item
	return p.playItem(item)
}

func (p *Player) playItem(item *queue.QueueItem) error {
	go func() {
		var audioSource string
		var useLocalFile bool

		if item.LocalFilePath != "" {
			if cachedPath, ok := p.cache.GetCachedFile(item.VideoID); ok {
				log.Printf("Using cached file: %s", cachedPath)
				audioSource = cachedPath
				useLocalFile = true
			}
		}

		if !useLocalFile {
			log.Printf("Downloading song: %s", item.Title)
			downloadedPath, err := p.cache.DownloadSong(item.VideoID)
			if err == nil {
				log.Printf("Downloaded song: %s -> %s", item.Title, downloadedPath)
				audioSource = downloadedPath
				useLocalFile = true
				p.queue.UpdateLocalPath(item.VideoID, downloadedPath)
			} else {
				log.Printf("Failed to download, falling back to stream: %s (error: %v)", item.Title, err)
				streamURL, streamErr := p.ytClient.GetStreamURL(item.VideoID)
				if streamErr != nil {
					log.Printf("Failed to get stream URL: %v", streamErr)
					return
				}
				audioSource = streamURL
			}
		}

		go p.preloadNextSongs()

		p.mu.Lock()
		if p.currentCmd != nil && p.currentCmd.Process != nil {
			if killErr := p.currentCmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				log.Printf("Failed to kill existing process: %v", killErr)
			}
			if waitErr := p.currentCmd.Wait(); waitErr != nil && !errors.Is(waitErr, os.ErrProcessDone) {
				log.Printf("Error waiting for killed process: %v", waitErr)
			}
		}

		ffplay := p.ffplayPath

		args := []string{
			"-i", audioSource,
			"-nodisp",
			"-autoexit",
			"-volume", fmt.Sprintf("%d", p.volume),
		}

		cmd := exec.Command(ffplay, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Start()
		if err != nil {
			log.Printf("Failed to start player: %v", err)
			p.mu.Unlock()
			return
		}

		p.currentCmd = cmd
		p.state = StatePlaying
		p.mu.Unlock()

		p.broadcastState()

		currentVideoID := item.VideoID
		shouldDeleteCache := useLocalFile

		go func(runningCmd *exec.Cmd, playedItem *queue.QueueItem, retryCount int) {
			err := runningCmd.Wait()

			p.mu.Lock()
			if p.currentCmd != runningCmd {
				p.mu.Unlock()
				return
			}

			p.currentCmd = nil
			p.state = StateStopped
			p.mu.Unlock()
			p.broadcastState()

			if err == nil {
				p.queue.Remove(p.queue.GetCurrentIndex())
				if shouldDeleteCache {
					log.Printf("Deleting cached file for: %s", currentVideoID)
					p.cache.Remove(currentVideoID)
				}
				p.Next()
			} else {
				if retryCount < 3 {
					log.Printf("Playback failed (attempt %d/3), retrying: %s (error: %v)", retryCount+1, playedItem.Title, err)
					p.playItemWithRetry(playedItem, retryCount+1)
				} else {
					log.Printf("Playback failed after 3 attempts, skipping: %s (error: %v)", playedItem.Title, err)
					p.queue.Remove(p.queue.GetCurrentIndex())
					if shouldDeleteCache {
						p.cache.Remove(currentVideoID)
					}
					p.Next()
				}
			}
		}(cmd, item, 0)
	}()

	return nil
}

func (p *Player) playItemWithRetry(item *queue.QueueItem, retryCount int) error {
	return p.playItem(item)
}

func (p *Player) stopCurrentProcess() {
	p.mu.Lock()
	if p.currentCmd != nil && p.currentCmd.Process != nil {
		if killErr := p.currentCmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			log.Printf("Failed to kill process: %v", killErr)
		}
		if waitErr := p.currentCmd.Wait(); waitErr != nil && !errors.Is(waitErr, os.ErrProcessDone) {
			log.Printf("Error waiting for process: %v", waitErr)
		}
		p.currentCmd = nil
	}
	p.mu.Unlock()
}

func (p *Player) Pause() error {
	p.mu.Lock()

	if p.state != StatePlaying {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	p.stopCurrentProcess()
	p.mu.Lock()
	p.state = StatePaused
	p.mu.Unlock()

	p.broadcastState()
	return nil
}

func (p *Player) Resume() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != StatePaused {
		return nil
	}

	if p.currentItem != nil {
		return p.playItem(p.currentItem)
	}

	return p.playLocked()
}

func (p *Player) Stop() error {
	p.stopCurrentProcess()
	p.mu.Lock()
	p.state = StateStopped
	p.currentItem = nil
	p.mu.Unlock()

	p.broadcastState()
	return nil
}

func (p *Player) Next() error {
	p.stopCurrentProcess()

	item := p.queue.Next()
	if item == nil {
		p.mu.Lock()
		p.state = StateStopped
		p.currentItem = nil
		p.mu.Unlock()
		p.broadcastState()
		return nil
	}

	p.mu.Lock()
	p.currentItem = item
	p.mu.Unlock()
	return p.playItem(item)
}

func (p *Player) Previous() error {
	p.stopCurrentProcess()

	item := p.queue.Previous()
	if item == nil {
		p.mu.Lock()
		p.state = StateStopped
		p.currentItem = nil
		p.mu.Unlock()
		p.broadcastState()
		return nil
	}

	p.mu.Lock()
	p.currentItem = item
	p.mu.Unlock()
	return p.playItem(item)
}

func (p *Player) SetVolume(vol int) error {
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}

	p.mu.Lock()
	p.volume = vol
	p.mu.Unlock()

	p.broadcastState()
	return nil
}

func (p *Player) PlayIndex(index int) error {
	p.stopCurrentProcess()

	item := p.queue.SetCurrent(index)
	if item == nil {
		return fmt.Errorf("invalid index")
	}

	p.mu.Lock()
	p.currentItem = item
	p.mu.Unlock()
	return p.playItem(item)
}

func (p *Player) AddToQueue(result ytmusic.SearchResult, clientIP string) error {
	err := p.queue.Add(result, clientIP)
	if err != nil {
		p.broadcastState()
		return err
	}
	p.broadcastState()
	return nil
}

func (p *Player) RemoveFromQueue(index int) {
	if index == p.queue.GetCurrentIndex() {
		p.stopCurrentProcess()
		p.queue.Remove(index)

		item := p.queue.Next()
		if item == nil {
			p.mu.Lock()
			p.state = StateStopped
			p.currentItem = nil
			p.mu.Unlock()
		} else {
			p.mu.Lock()
			p.currentItem = item
			p.mu.Unlock()
			p.playItem(item)
		}
	} else {
		p.queue.Remove(index)
	}
	p.broadcastState()
}

func (p *Player) ClearQueue() {
	p.queue.Clear()
	p.mu.Lock()
	if p.currentItem != nil {
		p.currentItem = nil
	}
	p.mu.Unlock()

	p.broadcastState()
}

func (p *Player) GetQueue() []queue.QueueItem {
	return p.queue.GetAll()
}

func (p *Player) MoveQueueItem(from, to int) {
	p.queue.Move(from, to)
	p.broadcastState()
}

func (p *Player) Shutdown() {
	p.cancel()
	p.Stop()
}

func (p *Player) GetYtClient() *ytmusic.Client {
	return p.ytClient
}

func (p *Player) Context() context.Context {
	return p.ctx
}

func (p *Player) preloadNextSongs() {
	if p.cache == nil || p.preloadCount <= 0 {
		return
	}

	allItems := p.queue.GetAll()
	currentIdx := p.queue.GetCurrentIndex()

	startIdx := currentIdx + 1
	endIdx := startIdx + p.preloadCount
	if endIdx > len(allItems) {
		endIdx = len(allItems)
	}

	for i := startIdx; i < endIdx; i++ {
		item := allItems[i]
		if item.VideoID == "" {
			continue
		}

		if cachedPath, ok := p.cache.GetCachedFile(item.VideoID); ok {
			log.Printf("Song already cached: %s -> %s", item.Title, cachedPath)
			p.queue.UpdateLocalPath(item.VideoID, cachedPath)
			continue
		}

		log.Printf("Preloading song: %s (videoID: %s)", item.Title, item.VideoID)
		downloadedPath, err := p.cache.DownloadSong(item.VideoID)
		if err != nil {
			log.Printf("Failed to preload song: %s - %v", item.Title, err)
			continue
		}

		log.Printf("Preloaded song: %s -> %s", item.Title, downloadedPath)
		p.queue.UpdateLocalPath(item.VideoID, downloadedPath)
	}
}
