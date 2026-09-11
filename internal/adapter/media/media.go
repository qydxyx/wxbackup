// Package media transcodes voice and video with an ffmpeg sidecar.
//
// The binary is resolved from WXBACKUP_FFMPEG, then PATH. Tests inject a stub
// command through that env var so they do not need a real encoder or media files.
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wxbackup/wxbackup/internal/domain"
)

const (
	EnvFFmpeg = "WXBACKUP_FFMPEG"

	FormatMP3 = "mp3"
	FormatMP4 = "mp4"
)

const CodeFFmpegMissing domain.Code = "ffmpeg_missing"

var (
	ErrFFmpegMissing = &domain.Error{
		Code:    CodeFFmpegMissing,
		Message: "ffmpeg binary is not on PATH",
	}
	ErrUnsupportedFormat = &domain.Error{
		Code:    domain.Code("invalid_request"),
		Message: "unsupported transcode format",
	}
)

// Transcode converts src to format ("mp3" or "mp4") via ffmpeg.
// A missing input file is domain.ErrMediaNeverOpened.
func Transcode(ctx context.Context, src, format string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != FormatMP3 && format != FormatMP4 {
		return nil, ErrUnsupportedFormat
	}
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, domain.ErrMediaNeverOpened
	}
	st, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrMediaNeverOpened
		}
		return nil, fmt.Errorf("media: stat: %w", err)
	}
	if st.IsDir() {
		return nil, domain.ErrMediaNeverOpened
	}

	bin, err := lookFFmpeg()
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "wxbackup-transcode-*")
	if err != nil {
		return nil, fmt.Errorf("media: temp: %w", err)
	}
	defer os.RemoveAll(dir)
	dest := filepath.Join(dir, "out."+format)

	args := ffmpegArgs(src, dest, format)
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("media: ffmpeg: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("media: ffmpeg: %w", err)
	}
	out, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("media: ffmpeg produced no output")
		}
		return nil, fmt.Errorf("media: read output: %w", err)
	}
	return out, nil
}

func TranscodeVoice(ctx context.Context, src string) ([]byte, error) {
	return Transcode(ctx, src, FormatMP3)
}

func TranscodeVideo(ctx context.Context, src string) ([]byte, error) {
	return Transcode(ctx, src, FormatMP4)
}

func lookFFmpeg() (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvFFmpeg)); v != "" {
		st, err := os.Stat(v)
		if err != nil || st.IsDir() {
			return "", ErrFFmpegMissing
		}
		return v, nil
	}
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", ErrFFmpegMissing
	}
	return p, nil
}

func ffmpegArgs(src, dest, format string) []string {
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y", "-i", src}
	switch format {
	case FormatMP3:
		args = append(args, "-vn", "-f", FormatMP3, dest)
	default:
		args = append(args, "-f", FormatMP4, dest)
	}
	return args
}
