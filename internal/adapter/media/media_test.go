package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func TestTranscodeMissingFile(t *testing.T) {
	t.Setenv(EnvFFmpeg, writeStub(t))
	_, err := Transcode(context.Background(), filepath.Join(t.TempDir(), "gone.bin"), FormatMP3)
	if !errors.Is(err, domain.ErrMediaNeverOpened) {
		t.Fatalf("got %v", err)
	}
	_, err = TranscodeVoice(context.Background(), "")
	if !errors.Is(err, domain.ErrMediaNeverOpened) {
		t.Fatalf("empty src: %v", err)
	}
}

func TestTranscodeMissingFFmpeg(t *testing.T) {
	src := writeSrc(t, []byte("pcm"))
	t.Setenv(EnvFFmpeg, filepath.Join(t.TempDir(), "no-ffmpeg"))
	_, err := TranscodeVoice(context.Background(), src)
	if !errors.Is(err, ErrFFmpegMissing) {
		t.Fatalf("got %v", err)
	}
	de, ok := domain.AsError(err)
	if !ok || de.Code != CodeFFmpegMissing {
		t.Fatalf("typed error %+v ok=%v", de, ok)
	}
}

func TestTranscodeMissingFFmpegOnPATH(t *testing.T) {
	src := writeSrc(t, []byte("pcm"))
	t.Setenv(EnvFFmpeg, "")
	t.Setenv("PATH", t.TempDir())
	_, err := TranscodeVideo(context.Background(), src)
	if !errors.Is(err, ErrFFmpegMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestTranscodeUnsupportedFormat(t *testing.T) {
	t.Setenv(EnvFFmpeg, writeStub(t))
	src := writeSrc(t, []byte("x"))
	_, err := Transcode(context.Background(), src, "ogg")
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("got %v", err)
	}
}

func TestTranscodeVoiceAndVideoWithStub(t *testing.T) {
	if os.Getenv(EnvFFmpeg) == "" {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			t.Log("ffmpeg not on PATH; using WXBACKUP_FFMPEG stub")
		}
	}
	t.Setenv(EnvFFmpeg, writeStub(t))
	src := writeSrc(t, []byte("silk-bytes"))

	mp3, err := TranscodeVoice(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mp3, []byte("STUB-mp3-silk-bytes")) {
		t.Fatalf("mp3 %q", mp3)
	}

	src2 := writeSrc(t, []byte("h264"))
	mp4, err := TranscodeVideo(context.Background(), src2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mp4, []byte("STUB-mp4-h264")) {
		t.Fatalf("mp4 %q", mp4)
	}
}

func TestTranscodeCanceled(t *testing.T) {
	t.Setenv(EnvFFmpeg, writeStub(t))
	src := writeSrc(t, []byte("x"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Transcode(ctx, src, FormatMP3)
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func writeSrc(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.bin")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeStub(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	script := `#!/bin/sh
input=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-i" ]; then
    input=$arg
  fi
  prev=$arg
done
output=$prev
fmt=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-f" ]; then
    fmt=$arg
  fi
  prev=$arg
done
if [ -z "$input" ] || [ ! -f "$input" ]; then
  echo "ffmpeg-stub: input missing" >&2
  exit 1
fi
if [ "$output" = "-" ]; then
  printf 'STUB-%s' "$fmt"
  exit 0
fi
printf 'STUB-%s-' "$fmt" > "$output"
cat "$input" >> "$output"
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
