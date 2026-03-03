package ytmusic

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

type SearchResult struct {
	VideoID   string `json:"video_id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Duration  int    `json:"duration"`
	Thumbnail string `json:"thumbnail"`
	URL       string `json:"url"`
}

type Client struct {
	timeout time.Duration
	ytDlp   string
}

func New() *Client {
	ytDlp := "yt-dlp"
	if os.PathSeparator == '\\' {
		ytDlp = "yt-dlp.exe"
	}
	return &Client{
		timeout: 30 * time.Second,
		ytDlp:   ytDlp,
	}
}

func (c *Client) Search(query string) ([]SearchResult, error) {
	args := []string{
		"--no-download",
		"--no-playlist",
		"--print", "%(id)s|%(title)s|%(artist)s|%(album)s|%(duration)s|%(thumbnail)s|%(url)s",
		"--default-search", "ytsearch5",
		"ytmusic:" + query,
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.ytDlp, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	results := parseSearchOutput(string(output))
	return results, nil
}

func parseSearchOutput(output string) []SearchResult {
	lines := regexp.MustCompile(`\r?\n`).Split(output, -1)
	results := make([]SearchResult, 0)

	for _, line := range lines {
		line = regexp.MustCompile(`^\s+|\s+$`).ReplaceAllString(line, "")
		if line == "" {
			continue
		}

		parts := regexp.MustCompile(`\|`).Split(line, -1)
		if len(parts) < 7 {
			continue
		}

		videoID := parts[0]
		title := parts[1]
		artist := parts[2]
		album := parts[3]
		duration := 0
		if parts[4] != "" {
			duration, _ = strconv.Atoi(parts[4])
		}
		thumbnail := parts[5]
		url := parts[6]

		if videoID != "" && title != "" {
			results = append(results, SearchResult{
				VideoID:   videoID,
				Title:     title,
				Artist:    artist,
				Album:     album,
				Duration:  duration,
				Thumbnail: thumbnail,
				URL:       url,
			})
		}
	}

	return results
}

func (c *Client) GetStreamURL(videoID string) (string, error) {
	args := []string{
		"-f", "bestaudio/best",
		"--get-url",
		"https://www.youtube.com/watch?v=" + videoID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.ytDlp, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	url := regexp.MustCompile(`^\s+|\s+$`).ReplaceAllString(string(output), "")
	return url, nil
}
