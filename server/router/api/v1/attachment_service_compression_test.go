package v1

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"

	"github.com/usememos/memos/internal/attachmentcompression"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

type fakeAttachmentCompressor struct {
	canCompress bool
	maxInput    int
	result      attachmentcompression.Result
	calls       int
}

func (c *fakeAttachmentCompressor) CanCompress(string) bool {
	return c.canCompress
}

func (c *fakeAttachmentCompressor) MaxInputBytes() int {
	return c.maxInput
}

func (c *fakeAttachmentCompressor) Compress(
	_ context.Context,
	_ []byte,
	_, _ string,
	onProgress func(int),
) (attachmentcompression.Result, error) {
	c.calls++
	onProgress(50)
	return c.result, nil
}

func TestCompressAttachmentUpdatesMediaAndBroadcastsPrivateProgress(t *testing.T) {
	hub := NewSSEHub()
	owner := hub.Subscribe(7, store.RoleUser)
	defer hub.Unsubscribe(owner)
	other := hub.Subscribe(8, store.RoleUser)
	defer hub.Unsubscribe(other)

	compressor := &fakeAttachmentCompressor{
		canCompress: true,
		maxInput:    1024,
		result: attachmentcompression.Result{
			Content:    []byte("smaller"),
			Filename:   "clip.mp4",
			MIMEType:   "video/mp4",
			Compressed: true,
		},
	}
	service := &APIV1Service{
		SSEHub:                    hub,
		mediaCompressionSemaphore: semaphore.NewWeighted(1),
		attachmentCompressor:      compressor,
	}
	attachment := &store.Attachment{
		UID:      "upload-id",
		Filename: "clip.mov",
		Type:     "video/quicktime",
		Blob:     []byte("original media"),
	}

	compressed, err := service.compressAttachment(context.Background(), attachment, 7)
	require.NoError(t, err)
	require.True(t, compressed)
	require.Equal(t, 1, compressor.calls)
	require.Equal(t, []byte("smaller"), attachment.Blob)
	require.Equal(t, int64(7), attachment.Size)
	require.Equal(t, "clip.mp4", attachment.Filename)
	require.Equal(t, "video/mp4", attachment.Type)

	var events []SSEEvent
	for len(owner.events) > 0 {
		var event SSEEvent
		require.NoError(t, json.Unmarshal(<-owner.events, &event))
		events = append(events, event)
	}
	require.Len(t, events, 3)
	require.Equal(t, "queued", events[0].Stage)
	require.Equal(t, "compressing", events[1].Stage)
	require.Equal(t, 52, events[2].Progress)
	require.Empty(t, other.events, "another user must not receive attachment progress")
}

func TestCompressAttachmentSkipsMotionMedia(t *testing.T) {
	compressor := &fakeAttachmentCompressor{canCompress: true, maxInput: 1024}
	service := &APIV1Service{
		mediaCompressionSemaphore: semaphore.NewWeighted(1),
		attachmentCompressor:      compressor,
	}
	attachment := &store.Attachment{
		Filename: "live.jpg",
		Type:     "image/jpeg",
		Blob:     []byte("motion media"),
		Payload: &storepb.AttachmentPayload{
			MotionMedia: &storepb.MotionMedia{GroupId: "paired"},
		},
	}

	compressed, err := service.compressAttachment(context.Background(), attachment, 7)
	require.NoError(t, err)
	require.False(t, compressed)
	require.Zero(t, compressor.calls)
}
