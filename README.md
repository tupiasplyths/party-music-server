# Music Bot - Windows Installation Guide

A server that plays music from YouTube Music, controllable from any device on your local network via a web browser.

## Prerequisites

1. **Go** (for building)
   - Download from: https://go.dev/dl/
   - Install Go 1.21 or later

2. **yt-dlp** (for streaming YouTube)
   - Download from: https://github.com/yt-dlp/yt-dlp/releases
   - Place `yt-dlp.exe` in the project folder

3. **ffmpeg** (for audio playback)
   - Download from: https://ffmpeg.org/download.html
   - Add ffmpeg to your PATH, or place `ffmpeg.exe` in the project folder
   - You need `ffplay.exe` (included with ffmpeg)

4. **Audio output**
   - Windows uses default audio device (no extra setup needed)

## Installation

### Option 1: Build from Source

1. Install Go from https://go.dev/dl/

2. Download this source code:
   ```powershell
   git clone <your-repo-url>
   cd music_bot
   ```

3. Download yt-dlp.exe:
   - Go to https://github.com/yt-dlp/yt-dlp/releases
   - Download the latest `yt-dlp.exe`
   - Place it in the project folder

4. Download ffmpeg:
   - Go to https://www.gyan.dev/ffmpeg/builds/
   - Download "ffmpeg-release-essentials.zip"
   - Extract and add ffmpeg to PATH, or copy ffmpeg.exe to project folder

5. Build:
   ```powershell
   go build -o musicbot.exe ./cmd/
   ```

### Option 2: Pre-built Binary

(If someone provides a compiled musicbot.exe)

1. Download musicbot.exe
2. Download yt-dlp.exe
3. Download ffmpeg.exe (or ensure ffmpeg is in PATH)
4. Place all files in the same folder

## Configuration

Edit `config.yaml` to customize:

```yaml
server:
  host: "0.0.0.0"  # Listen on all interfaces
  port: 8080       # Web server port

music:
  output_device: "default"  # Audio device
  volume: 80               # Default volume (0-100)
```

## Usage

### Starting the Server

```powershell
.\musicbot.exe
```

The server will start on `http://localhost:8080`

### Accessing from Other Devices

1. Find your Windows PC's IP address:
   ```powershell
   ipconfig
   ```
   Look for IPv4 Address (e.g., `192.168.1.100`)

2. On other devices (phones, tablets, other computers), open:
   ```
   http://192.168.1.100:8080
   ```

### Features

- **Search**: Type a song name and click Search
- **Add to Queue**: Click "Add" on search results
- **Playback Controls**: Play, pause, stop, next, previous
- **Volume**: Adjust with slider
- **Queue Management**: Click a song to play it, or click X to remove

### Firewall

If you can't access from other devices, allow the port:

```powershell
netsh advfirewall firewall add rule name="MusicBot" dir=in action=allow protocol=tcp localport=8080
```

## Troubleshooting

### "ffmpeg/ffplay not found"
- Ensure ffplay.exe is in the same folder as musicbot.exe, or in your PATH
- ffplay.exe comes with ffmpeg

### "yt-dlp not found"
- Ensure yt-dlp.exe is in the same folder as musicbot.exe

### "Port already in use"
- Change the port in config.yaml to something else (e.g., 8081)

### Can't access from phone/tablet
1. Check firewall rules above
2. Make sure both devices are on the same WiFi network
3. Verify your IP address is correct

### No audio output
- Check Windows volume settings
- Make sure the correct output device is selected

## Project Structure

```
music_bot/
├── musicbot.exe      # Main program
├── yt-dlp.exe        # YouTube downloader
├── ffplay.exe        # Audio player (comes with ffmpeg)
├── config.yaml       # Configuration
├── web/              # Web interface files
├── queue.json        # Queue data (created automatically)
└── README.md         # This file
```

**Important**: Make sure `yt-dlp.exe` and `ffplay.exe` are in the same folder as `musicbot.exe`!
