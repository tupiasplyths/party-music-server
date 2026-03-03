package player

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
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
}

type PlaybackState string

const (
	StateStopped PlaybackState = "stopped"
	StatePlaying PlaybackState = "playing"
	StatePaused  PlaybackState = "paused"
)

func New(q *queue.Queue, volume int, device string) *Player {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Player{
		queue:  q,
		volume: volume,
		device: device,
		state:  StateStopped,
		ctx:    ctx,
		cancel: cancel,
	}
	p.ytClient = ytmusic.New()

	return p
}

func (p *Player) SetBroadcast(fn func(interface{})) {
	p.broadcast = fn
}

func (p *Player) broadcastState() {
	if p.broadcast != nil {
		p.broadcast(p.GetState())
	}
}

func (p *Player) GetState() PlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	state := PlayerState{
		State:        p.state,
		Volume:       p.volume,
		CurrentIndex: p.queue.GetCurrentIndex(),
	}

	if p.currentItem != nil {
		state.Current = &NowPlaying{
			Title:     p.currentItem.Title,
			Artist:    p.currentItem.Artist,
			Thumbnail: p.currentItem.Thumbnail,
			Duration:  p.currentItem.Duration,
		}
	}

	state.Queue = p.queue.GetAll()
	state.CurrentIndex = p.queue.GetCurrentIndex()

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

func getAudioPlayer() string {
	if runtime.GOOS == "windows" {
		return "ffplay.exe"
	}
	return "ffplay"
}

func getYtDlp() string {
	if runtime.GOOS == "windows" {
		return "yt-dlp.exe"
	}
	return "yt-dlp"
}

func (p *Player) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()

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
	streamURL, err := p.ytClient.GetStreamURL(item.VideoID)
	if err != nil {
		return fmt.Errorf("failed to get stream URL: %w", err)
	}

	if p.currentCmd != nil && p.currentCmd.Process != nil {
		p.currentCmd.Process.Kill()
	}

	ffplay := getAudioPlayer()

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
		return err
	}

	p.currentCmd = cmd
	p.state = StatePlaying
	p.broadcastState()

	go func() {
		cmd.Wait()
		p.mu.Lock()
		if p.state == StatePlaying {
			p.state = StateStopped
			p.broadcastState()
		}
		p.mu.Unlock()
	}()

	return nil
}

func (p *Player) Pause() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != StatePlaying {
		return nil
	}

	if p.currentCmd != nil && p.currentCmd.Process != nil {
		p.currentCmd.Process.Kill()
	}
	p.state = StatePaused
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

	return p.Play()
}

func (p *Player) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentCmd != nil && p.currentCmd.Process != nil {
		p.currentCmd.Process.Kill()
	}
	p.currentCmd = nil
	p.state = StateStopped
	p.broadcastState()
	return nil
}

func (p *Player) Next() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentCmd != nil && p.currentCmd.Process != nil {
		p.currentCmd.Process.Kill()
	}

	item := p.queue.Next()
	if item == nil {
		p.state = StateStopped
		p.currentItem = nil
		p.broadcastState()
		return nil
	}

	p.currentItem = item
	return p.playItem(item)
}

func (p *Player) Previous() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentCmd != nil && p.currentCmd.Process != nil {
		p.currentCmd.Process.Kill()
	}

	item := p.queue.Previous()
	if item == nil {
		p.state = StateStopped
		p.currentItem = nil
		p.broadcastState()
		return nil
	}

	p.currentItem = item
	return p.playItem(item)
}

func (p *Player) SetVolume(vol int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}

	p.volume = vol
	p.broadcastState()
	return nil
}

func (p *Player) PlayIndex(index int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentCmd != nil && p.currentCmd.Process != nil {
		p.currentCmd.Process.Kill()
	}

	item := p.queue.SetCurrent(index)
	if item == nil {
		return fmt.Errorf("invalid index")
	}

	p.currentItem = item
	return p.playItem(item)
}

func (p *Player) AddToQueue(result ytmusic.SearchResult) {
	p.queue.Add(result)
	p.broadcastState()
}

func (p *Player) RemoveFromQueue(index int) {
	p.queue.Remove(index)
	p.broadcastState()
}

func (p *Player) ClearQueue() {
	p.queue.Clear()
	p.broadcastState()
}

func (p *Player) GetQueue() []queue.QueueItem {
	return p.queue.GetAll()
}

func (p *Player) Shutdown() {
	p.cancel()
	p.Stop()
}
