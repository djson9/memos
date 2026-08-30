package attachmentcompression

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

const (
	defaultMaxInputBytes       = 200 << 20
	defaultImageMaxDimension   = 2560
	defaultImageQuality        = 82
	defaultVideoMaxDimension   = 1920
	defaultVideoCRF            = 24
	defaultVideoPreset         = "medium"
	defaultCPULimitPercent     = 150
	minimumImageInputBytes     = 512 << 10
	minimumVideoInputBytes     = 5 << 20
	minimumAbsoluteSavings     = 256 << 10
	minimumRelativeSavingsRate = 5
	maxCommandErrorBytes       = 4096
)

var imageTypes = map[string]bool{
	"image/heic": true,
	"image/heif": true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/png":  true,
	"image/tiff": true,
	"image/webp": true,
}

var videoTypes = map[string]bool{
	"video/3gpp":      true,
	"video/mp4":       true,
	"video/mpeg":      true,
	"video/quicktime": true,
	"video/webm":      true,
	"video/x-m4v":     true,
}

// Config controls attachment compression.
type Config struct {
	Enabled         bool
	MaxInputBytes   int
	CPULimitPercent int
}

// Result is the attachment content and metadata produced by compression.
type Result struct {
	Content    []byte
	Filename   string
	MIMEType   string
	Compressed bool
}

type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	RunWithProgress(ctx context.Context, name string, args []string, onLine func(string)) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	return commandError(name, output, err)
}

func (execCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, commandError(name, stderr.Bytes(), err)
	}
	return output, nil
}

func (execCommandRunner) RunWithProgress(ctx context.Context, name string, args []string, onLine func(string)) error {
	command := exec.CommandContext(ctx, name, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return errors.Wrap(err, "failed to open command progress stream")
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return errors.Wrapf(err, "failed to start %s", name)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return errors.Wrap(err, "failed to read command progress")
	}
	return commandError(name, stderr.Bytes(), command.Wait())
}

func commandError(name string, output []byte, err error) error {
	if err == nil {
		return nil
	}
	if len(output) > maxCommandErrorBytes {
		output = output[len(output)-maxCommandErrorBytes:]
	}
	return errors.Wrapf(err, "%s failed: %s", name, strings.TrimSpace(string(output)))
}

// Compressor uses libvips and FFmpeg to shrink static images and videos.
type Compressor struct {
	config Config
	runner commandRunner
}

// New creates an attachment compressor.
func New(config Config) *Compressor {
	if config.MaxInputBytes <= 0 {
		config.MaxInputBytes = defaultMaxInputBytes
	}
	if config.CPULimitPercent <= 0 {
		config.CPULimitPercent = defaultCPULimitPercent
	}
	return &Compressor{
		config: config,
		runner: execCommandRunner{},
	}
}

// CanCompress reports whether compression is enabled for a MIME type.
func (c *Compressor) CanCompress(mimeType string) bool {
	return c != nil && c.config.Enabled && (imageTypes[mimeType] || videoTypes[mimeType])
}

// MaxInputBytes returns the largest source file accepted by the compressor.
func (c *Compressor) MaxInputBytes() int {
	if c == nil {
		return 0
	}
	return c.config.MaxInputBytes
}

// Compress transforms supported media and keeps the result only when it is materially smaller.
func (c *Compressor) Compress(ctx context.Context, content []byte, filename, mimeType string, onProgress func(int)) (Result, error) {
	original := Result{Content: content, Filename: filename, MIMEType: mimeType}
	if !c.CanCompress(mimeType) {
		return original, nil
	}
	if len(content) > c.config.MaxInputBytes {
		return original, errors.Errorf("media input exceeds compression limit of %d bytes", c.config.MaxInputBytes)
	}
	if imageTypes[mimeType] && len(content) < minimumImageInputBytes {
		return original, nil
	}
	if videoTypes[mimeType] && len(content) < minimumVideoInputBytes {
		return original, nil
	}
	reportProgress(onProgress, 2)

	workDir, err := os.MkdirTemp("", "memos-media-")
	if err != nil {
		return original, errors.Wrap(err, "failed to create media compression directory")
	}
	defer os.RemoveAll(workDir)

	inputPath := filepath.Join(workDir, "input"+inputExtension(mimeType))
	if err := os.WriteFile(inputPath, content, 0600); err != nil {
		return original, errors.Wrap(err, "failed to stage media input")
	}

	var outputPath, outputMIMEType, outputExtension string
	if imageTypes[mimeType] {
		outputPath, outputMIMEType, outputExtension, err = c.compressImage(ctx, workDir, inputPath, mimeType, onProgress)
	} else {
		outputPath, outputMIMEType, outputExtension, err = c.compressVideo(ctx, workDir, inputPath, onProgress)
	}
	if err != nil {
		return original, err
	}

	compressedContent, err := os.ReadFile(outputPath)
	if err != nil {
		return original, errors.Wrap(err, "failed to read compressed media")
	}
	if !hasMaterialSavings(len(content), len(compressedContent)) {
		reportProgress(onProgress, 100)
		return original, nil
	}
	reportProgress(onProgress, 100)

	return Result{
		Content:    compressedContent,
		Filename:   replaceExtension(filename, outputExtension),
		MIMEType:   outputMIMEType,
		Compressed: true,
	}, nil
}

func (c *Compressor) compressImage(
	ctx context.Context,
	workDir, inputPath, inputMIMEType string,
	onProgress func(int),
) (string, string, string, error) {
	outputMIMEType := inputMIMEType
	outputExtension := inputExtension(inputMIMEType)
	outputOptions := ""

	switch inputMIMEType {
	case "image/jpeg", "image/jpg", "image/heic", "image/heif", "image/tiff":
		outputMIMEType = "image/jpeg"
		outputExtension = ".jpg"
		outputOptions = fmt.Sprintf("Q=%d,strip,optimize-coding,interlace", defaultImageQuality)
	case "image/png":
		outputOptions = "compression=9,strip"
	case "image/webp":
		outputOptions = fmt.Sprintf("Q=%d,strip,effort=6", defaultImageQuality)
	default:
		return "", "", "", errors.Errorf("unsupported image MIME type %q", inputMIMEType)
	}

	outputPath := filepath.Join(workDir, "output"+outputExtension)
	outputSpec := outputPath + "[" + outputOptions + "]"
	args := []string{
		inputPath,
		"--size", strconv.Itoa(defaultImageMaxDimension) + "x" + strconv.Itoa(defaultImageMaxDimension),
		"--output", outputSpec,
		"--vips-concurrency=2",
		"--vips-disc-threshold=67108864",
	}
	reportProgress(onProgress, 10)
	command, commandArgs := c.throttledCommand("vipsthumbnail", args)
	if err := c.runner.Run(ctx, command, commandArgs...); err != nil {
		return "", "", "", errors.Wrap(err, "failed to compress image")
	}
	reportProgress(onProgress, 90)
	return outputPath, outputMIMEType, outputExtension, nil
}

func (c *Compressor) compressVideo(
	ctx context.Context,
	workDir, inputPath string,
	onProgress func(int),
) (string, string, string, error) {
	outputPath := filepath.Join(workDir, "output.mp4")
	durationSeconds := c.probeVideoDuration(ctx, inputPath)
	videoFilter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2",
		defaultVideoMaxDimension,
		defaultVideoMaxDimension,
	)
	args := []string{
		"-y",
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-progress", "pipe:1",
		"-nostats",
		"-i", inputPath,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-vf", videoFilter,
		"-c:v", "libx264",
		"-preset", defaultVideoPreset,
		"-crf", strconv.Itoa(defaultVideoCRF),
		"-threads", "2",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		"-movflags", "+faststart",
		"-map_metadata", "-1",
		"-map_chapters", "-1",
		"-max_muxing_queue_size", "1024",
		"-f", "mp4",
		outputPath,
	}
	reportProgress(onProgress, 10)
	command, commandArgs := c.throttledCommand("ffmpeg", args)
	if err := c.runner.RunWithProgress(ctx, command, commandArgs, func(line string) {
		reportFFmpegProgress(line, durationSeconds, onProgress)
	}); err != nil {
		return "", "", "", errors.Wrap(err, "failed to compress video")
	}
	reportProgress(onProgress, 95)
	return outputPath, "video/mp4", ".mp4", nil
}

func (c *Compressor) probeVideoDuration(ctx context.Context, inputPath string) float64 {
	output, err := c.runner.Output(
		ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)
	if err != nil {
		return 0
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || duration <= 0 {
		return 0
	}
	return duration
}

func (c *Compressor) throttledCommand(name string, args []string) (string, []string) {
	commandArgs := []string{"--limit", strconv.Itoa(c.config.CPULimitPercent), "--", name}
	commandArgs = append(commandArgs, args...)
	return "cpulimit", commandArgs
}

func reportFFmpegProgress(line string, durationSeconds float64, onProgress func(int)) {
	if durationSeconds <= 0 || !strings.HasPrefix(line, "out_time_us=") {
		return
	}
	outTimeMicroseconds, err := strconv.ParseInt(strings.TrimPrefix(line, "out_time_us="), 10, 64)
	if err != nil || outTimeMicroseconds < 0 {
		return
	}
	percent := 10 + int(float64(outTimeMicroseconds)/(durationSeconds*1_000_000)*80)
	if percent > 90 {
		percent = 90
	}
	reportProgress(onProgress, percent)
}

func reportProgress(onProgress func(int), percent int) {
	if onProgress != nil {
		onProgress(percent)
	}
}

func hasMaterialSavings(originalSize, compressedSize int) bool {
	if compressedSize <= 0 || compressedSize >= originalSize {
		return false
	}
	requiredSavings := originalSize * minimumRelativeSavingsRate / 100
	if requiredSavings < minimumAbsoluteSavings {
		requiredSavings = minimumAbsoluteSavings
	}
	return originalSize-compressedSize >= requiredSavings
}

func inputExtension(mimeType string) string {
	switch mimeType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/heic":
		return ".heic"
	case "image/heif":
		return ".heif"
	case "image/tiff":
		return ".tiff"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	case "video/x-m4v":
		return ".m4v"
	case "video/3gpp":
		return ".3gp"
	case "video/mpeg":
		return ".mpeg"
	default:
		return ".bin"
	}
}

func replaceExtension(filename, extension string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		base = "attachment"
	}
	return base + extension
}
