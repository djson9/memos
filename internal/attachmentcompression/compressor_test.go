package attachmentcompression

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeCommandRunner struct {
	command string
	args    []string
	output  []byte
	stdout  []byte
	err     error
}

func (r *fakeCommandRunner) Run(_ context.Context, name string, args ...string) error {
	r.command = name
	r.args = append([]string(nil), args...)
	if r.err != nil {
		return r.err
	}
	outputPath := args[len(args)-1]
	if name == "vipsthumbnail" || slices.Contains(args, "vipsthumbnail") {
		for i, arg := range args {
			if arg == "--output" && i+1 < len(args) {
				outputPath = strings.SplitN(args[i+1], "[", 2)[0]
				break
			}
		}
	}
	return os.WriteFile(outputPath, r.output, 0600)
}

func (r *fakeCommandRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	r.command = name
	r.args = append([]string(nil), args...)
	if r.err != nil {
		return nil, r.err
	}
	if len(r.stdout) > 0 {
		return r.stdout, nil
	}
	return []byte("10.0\n"), nil
}

func (r *fakeCommandRunner) RunWithProgress(_ context.Context, name string, args []string, onLine func(string)) error {
	r.command = name
	r.args = append([]string(nil), args...)
	if r.err != nil {
		return r.err
	}
	onLine("out_time_us=5000000")
	return os.WriteFile(args[len(args)-1], r.output, 0600)
}

func TestCompressJPEG(t *testing.T) {
	runner := &fakeCommandRunner{output: make([]byte, 100<<10)}
	compressor := New(Config{Enabled: true, MaxInputBytes: 2 << 20})
	compressor.runner = runner

	result, err := compressor.Compress(context.Background(), make([]byte, 1<<20), "photo.jpeg", "image/jpeg", nil)
	require.NoError(t, err)
	require.True(t, result.Compressed)
	require.Equal(t, "photo.jpg", result.Filename)
	require.Equal(t, "image/jpeg", result.MIMEType)
	require.Equal(t, "cpulimit", runner.command)
	require.Contains(t, runner.args, "vipsthumbnail")
	require.Contains(t, runner.args, "2560x2560")
}

func TestCompressHEICToJPEG(t *testing.T) {
	runner := &fakeCommandRunner{output: make([]byte, 100<<10)}
	compressor := New(Config{Enabled: true, MaxInputBytes: 2 << 20})
	compressor.runner = runner

	result, err := compressor.Compress(context.Background(), make([]byte, 1<<20), "camera.heic", "image/heic", nil)
	require.NoError(t, err)
	require.True(t, result.Compressed)
	require.Equal(t, "camera.jpg", result.Filename)
	require.Equal(t, "image/jpeg", result.MIMEType)
}

func TestCompressVideoToMP4(t *testing.T) {
	runner := &fakeCommandRunner{output: make([]byte, 1<<20)}
	compressor := New(Config{Enabled: true, MaxInputBytes: 20 << 20})
	compressor.runner = runner

	var progress []int
	result, err := compressor.Compress(context.Background(), make([]byte, 10<<20), "clip.mov", "video/quicktime", func(percent int) {
		progress = append(progress, percent)
	})
	require.NoError(t, err)
	require.True(t, result.Compressed)
	require.Equal(t, "clip.mp4", result.Filename)
	require.Equal(t, "video/mp4", result.MIMEType)
	require.Equal(t, "cpulimit", runner.command)
	require.Contains(t, runner.args, "ffmpeg")
	require.Contains(t, runner.args, "libx264")
	require.Contains(t, runner.args, "+faststart")
	require.Contains(t, progress, 50)
	require.Equal(t, 100, progress[len(progress)-1])
}

func TestKeepsOriginalWithoutMaterialSavings(t *testing.T) {
	runner := &fakeCommandRunner{output: make([]byte, 1000<<10)}
	compressor := New(Config{Enabled: true, MaxInputBytes: 2 << 20})
	compressor.runner = runner
	original := make([]byte, 1<<20)

	result, err := compressor.Compress(context.Background(), original, "photo.jpg", "image/jpeg", nil)
	require.NoError(t, err)
	require.False(t, result.Compressed)
	require.Equal(t, original, result.Content)
	require.Equal(t, "photo.jpg", result.Filename)
}

func TestRejectsInputAboveCompressionLimit(t *testing.T) {
	compressor := New(Config{Enabled: true, MaxInputBytes: 1 << 20})

	_, err := compressor.Compress(context.Background(), make([]byte, (1<<20)+1), "photo.jpg", "image/jpeg", nil)
	require.ErrorContains(t, err, "media input exceeds compression limit")
}

func TestDisabledCompressorKeepsOriginal(t *testing.T) {
	compressor := New(Config{Enabled: false})
	original := make([]byte, 1<<20)

	result, err := compressor.Compress(context.Background(), original, "photo.jpg", "image/jpeg", nil)
	require.NoError(t, err)
	require.False(t, result.Compressed)
	require.Equal(t, original, result.Content)
}
