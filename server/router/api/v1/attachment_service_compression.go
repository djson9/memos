package v1

import (
	"context"

	"github.com/usememos/memos/internal/attachmentcompression"
	"github.com/usememos/memos/store"
)

type attachmentCompressor interface {
	CanCompress(mimeType string) bool
	MaxInputBytes() int
	Compress(
		ctx context.Context,
		content []byte,
		filename, mimeType string,
		onProgress func(int),
	) (attachmentcompression.Result, error)
}

func (s *APIV1Service) canCompressAttachment(attachment *store.Attachment) bool {
	if s.attachmentCompressor == nil || attachment == nil || !s.attachmentCompressor.CanCompress(attachment.Type) {
		return false
	}
	// Live Photos and Motion Photos carry pairing or embedded-video metadata that
	// must not be rewritten independently.
	return getAttachmentMotionMedia(attachment) == nil
}

func (s *APIV1Service) attachmentFitsCompressionInput(attachment *store.Attachment) bool {
	return s.canCompressAttachment(attachment) && len(attachment.Blob) <= s.attachmentCompressor.MaxInputBytes()
}

func (s *APIV1Service) compressAttachment(ctx context.Context, attachment *store.Attachment, creatorID int32) (bool, error) {
	if !s.attachmentFitsCompressionInput(attachment) {
		return false, nil
	}

	s.broadcastAttachmentProgress(attachment, creatorID, "queued", 10)
	release, err := s.acquireMediaCompressionSlot(ctx)
	if err != nil {
		return false, err
	}
	defer release()

	s.broadcastAttachmentProgress(attachment, creatorID, "compressing", 15)
	result, err := s.attachmentCompressor.Compress(
		ctx,
		attachment.Blob,
		attachment.Filename,
		attachment.Type,
		func(percent int) {
			mappedPercent := 15 + percent*75/100
			s.broadcastAttachmentProgress(attachment, creatorID, "compressing", mappedPercent)
		},
	)
	if err != nil {
		return false, err
	}
	if !result.Compressed {
		return false, nil
	}

	attachment.Blob = result.Content
	attachment.Size = int64(len(result.Content))
	attachment.Filename = result.Filename
	attachment.Type = result.MIMEType
	return true, nil
}

func (s *APIV1Service) acquireMediaCompressionSlot(ctx context.Context) (func(), error) {
	if s.mediaCompressionSemaphore == nil {
		return func() {}, nil
	}
	if err := s.mediaCompressionSemaphore.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() {
		s.mediaCompressionSemaphore.Release(1)
	}, nil
}

func (s *APIV1Service) broadcastAttachmentProgress(attachment *store.Attachment, creatorID int32, stage string, progress int) {
	if s.SSEHub == nil || attachment == nil {
		return
	}
	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}
	s.SSEHub.Broadcast(&SSEEvent{
		Type:       SSEEventAttachmentProgress,
		Name:       AttachmentNamePrefix + attachment.UID,
		Filename:   attachment.Filename,
		Stage:      stage,
		Progress:   progress,
		Visibility: store.Private,
		CreatorID:  creatorID,
	})
}
