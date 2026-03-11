package player

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"

	"musicbot/queue"
	"musicbot/ytmusic"
)

type Player struct {
	mu          sync.RWMutex
	queue       *queue.Queue
	ytClient    *ytmusic.Client
	state       PlaybackState
	volume      int
	device      string
	currentItem *queue.QueueItem
	currentCmd  *exec.Cmd
	broadcast   func(interface{})
	ctx         context.Context
	cancel      context.CancelFunc
	ytDlpPath   string
	ffplayPath  string
}

type PlaybackState string

const (
	StateStopped PlaybackState = "stopped"
	StatePlaying PlaybackState = "playing"
	StatePaused  PlaybackState = "paused"
)

func New(q *queue.Queue, volume int, device string, ytDlpPath, ffplayPath string) *Player {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Player{
		queue:      q,
		volume:     volume,
		device:     device,
		state:      StateStopped,
		ctx:        ctx,
		cancel:     cancel,
		ytDlpPath:  ytDlpPath,
		ffplayPath: ffplayPath,
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
		log.Printf("DEBUG: Broadcasting state - Queue length: %d, Current: %v\n", len(state.Queue), state.Current)
		log.Println("DEBUG: About to broadcast to all clients")
		p.broadcast(state)
		log.Println("DEBUG: Broadcast completed")
	}
}

func (p *Player) GetState() PlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.getStateLocked()
}

func (p *Player) getStateLocked() PlayerState {
	queueItems := p.queue.GetAll()
	currentIndex := p.queue.GetCurrentIndex()

	state := PlayerState{
		State:        p.state,
		Volume:       p.volume,
		CurrentIndex: currentIndex,
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
	State        PlaybackState     `json:"state"`
	Volume       int               `json:"volume"`
	Current      *NowPlaying       `json:"current,omitempty"`
	Queue        []queue.QueueItem `json:"queue"`
	CurrentIndex int               `json:"current_index"`
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
		streamURL, err := p.ytClient.GetStreamURL(item.VideoID)
		if err != nil {
			log.Printf("Failed to get stream URL: %v", err)
			return
		}

		p.mu.Lock()
		if p.currentCmd != nil && p.currentCmd.Process != nil {
			if killErr := p.currentCmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				log.Printf("Failed to kill existing process: %v", killErr)
			}
			// Wait for the process to ensure proper cleanup
			if waitErr := p.currentCmd.Wait(); waitErr != nil && !errors.Is(waitErr, os.ErrProcessDone) {
				log.Printf("Error waiting for killed process: %v", waitErr)
			}
		}

		ffplay := p.ffplayPath

		args := []string{
			"-i", streamURL,
			"-nodisp",
			"-autoexit",
			"-volume", fmt.Sprintf("%d", p.volume),
		}

		cmd := exec.Command(ffplay, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Start()
		if err != nil {
			log.Printf("Failed to start player: %v", err)
			p.mu.Unlock()
			return
		}

		p.currentCmd = cmd
		p.state = StatePlaying
		p.mu.Unlock()

		p.broadcastState()

		go func() {
			_ = cmd.Wait()
			p.mu.Lock()
			wasPlaying := p.state == StatePlaying
			state := p.state
			currentItem := p.currentItem
			p.currentCmd = nil
			p.state = StateStopped
			p.mu.Unlock()
			p.broadcastState()

			if wasPlaying && currentItem != nil && state == StatePlaying {
				log.Printf("Playback interrupted, retrying: %s", currentItem.Title)
				p.playItem(currentItem)
			}
		}()
	}()

	return nil
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
	p.mu.Lock()
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}

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

func (p *Player) AddToQueue(result ytmusic.SearchResult) {
	log.Printf("DEBUG: AddToQueue called - Title: %s, VideoID: %s\n", result.Title, result.VideoID)
	p.queue.Add(result)
	log.Println("DEBUG: AddToQueue - Item added to queue, calling broadcastState")
	p.broadcastState()
}

func (p *Player) RemoveFromQueue(index int) {
	p.queue.Remove(index)
	p.broadcastState()
}

func (p *Player) ClearQueue() {
	log.Println("DEBUG: ClearQueue called")
	p.queue.Clear()
	log.Println("DEBUG: Queue cleared, broadcasting state")
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
