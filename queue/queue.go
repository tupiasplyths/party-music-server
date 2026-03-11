package queue

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"musicbot/ytmusic"
)

type Queue struct {
	mu       sync.RWMutex
	Items    []QueueItem `json:"items"`
	Current  int         `json:"current"`
	Filename string      `json:"-"`
}

type QueueItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Duration  int    `json:"duration"` // seconds
	Thumbnail string `json:"thumbnail"`
	VideoID   string `json:"video_id"`
	URL       string `json:"url"`
}

func New(filename string) *Queue {
	q := &Queue{
		Filename: filename,
		Items:    make([]QueueItem, 0),
		Current:  -1,
	}
	q.load()
	return q
}

func (q *Queue) load() {
	if q.Filename == "" {
		return
	}
	data, err := os.ReadFile(q.Filename)
	if err != nil {
		return
	}
	json.Unmarshal(data, q)
}

func (q *Queue) Save() {
	if q.Filename == "" {
		return
	}
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		log.Printf("ERROR: Failed to marshal queue: %v", err)
		return
	}
	if err := os.WriteFile(q.Filename, data, 0644); err != nil {
		log.Printf("ERROR: Failed to write queue file: %v", err)
	}
}

func (q *Queue) Add(item ytmusic.SearchResult) {
	q.mu.Lock()
	defer q.mu.Unlock()

	qi := QueueItem{
		ID:        item.VideoID,
		Title:     item.Title,
		Artist:    item.Artist,
		Album:     item.Album,
		Duration:  item.Duration,
		Thumbnail: item.Thumbnail,
		VideoID:   item.VideoID,
		URL:       item.URL,
	}
	q.Items = append(q.Items, qi)
	q.Save()
}

func (q *Queue) Remove(index int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if index < 0 || index >= len(q.Items) {
		return
	}

	q.Items = append(q.Items[:index], q.Items[index+1:]...)
	if q.Current >= index {
		q.Current--
	}
	q.Save()
}

func (q *Queue) GetCurrent() *QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.Current < 0 || q.Current >= len(q.Items) {
		return nil
	}
	item := q.Items[q.Current]
	return &item
}

func (q *Queue) Next() *QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.Items) == 0 {
		return nil
	}

	q.Current++
	if q.Current >= len(q.Items) {
		q.Current = len(q.Items) - 1
		return nil
	}

	item := q.Items[q.Current]
	q.Save()
	return &item
}

func (q *Queue) Previous() *QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.Items) == 0 {
		return nil
	}

	q.Current--
	if q.Current < 0 {
		q.Current = 0
	}
	item := q.Items[q.Current]
	q.Save()
	return &item
}

func (q *Queue) SetCurrent(index int) *QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	if index < 0 || index >= len(q.Items) {
		return nil
	}

	q.Current = index
	item := q.Items[q.Current]
	q.Save()
	return &item
}

func (q *Queue) GetAll() []QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := make([]QueueItem, len(q.Items))
	copy(result, q.Items)
	return result
}

func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	log.Printf("DEBUG: Queue.Clear called - clearing %d items, current index: %d\n", len(q.Items), q.Current)
	q.Items = make([]QueueItem, 0)
	q.Current = -1
	q.Save()
	log.Printf("DEBUG: Queue.Clear completed - items: %d, current index: %d\n", len(q.Items), q.Current)
}

func (q *Queue) GetCurrentIndex() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.Current
}

func (q *Queue) Move(from, to int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if from < 0 || from >= len(q.Items) || to < 0 || to >= len(q.Items) || from == to {
		return
	}

	item := q.Items[from]
	q.Items = append(q.Items[:from], q.Items[from+1:]...)
	q.Items = append(q.Items[:to], append([]QueueItem{item}, q.Items[to:]...)...)

	if q.Current == from {
		q.Current = to
	} else if from < q.Current && to < q.Current {
		// Item moved from before current to before current, no change needed
	} else if from < q.Current && to > q.Current {
		// Item moved from before current to after current
		q.Current--
	} else if from > q.Current && to <= q.Current {
		// Item moved from after current to before or at current
		q.Current++
	}

	q.Save()
}
