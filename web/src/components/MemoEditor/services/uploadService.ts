import { create } from "@bufbuild/protobuf";
import { attachmentServiceClient } from "@/connect";
import type { Attachment } from "@/types/proto/api/v1/attachment_service_pb";
import { AttachmentSchema, MotionMediaSchema } from "@/types/proto/api/v1/attachment_service_pb";
import { subscribeAttachmentProgress } from "@/utils/attachmentProgress";
import type { LocalFile, UploadProgress, UploadProgressStage } from "../types/attachment";

type UploadProgressCallback = (progress: UploadProgress) => void;

function reportProgress(
  callback: UploadProgressCallback | undefined,
  filename: string,
  stage: UploadProgressStage,
  filePercent: number,
  fileIndex: number,
  totalFiles: number,
) {
  if (!callback) return;
  const normalizedFilePercent = Math.max(0, Math.min(100, filePercent));
  callback({
    filename,
    stage,
    filePercent: normalizedFilePercent,
    overallPercent: Math.round(((fileIndex + normalizedFilePercent / 100) / totalFiles) * 100),
    currentFile: fileIndex + 1,
    totalFiles,
  });
}

export const uploadService = {
  async uploadFile(localFile: LocalFile, attachmentId?: string): Promise<Attachment> {
    const { file, motionMedia } = localFile;
    const [mediaMetadata, arrayBuffer] = await Promise.all([localFile.mediaMetadata, file.arrayBuffer()]);
    const buffer = new Uint8Array(arrayBuffer);
    return attachmentServiceClient.createAttachment({
      attachment: create(AttachmentSchema, {
        filename: file.name,
        size: BigInt(file.size),
        type: file.type,
        content: buffer,
        motionMedia: motionMedia ? create(MotionMediaSchema, motionMedia) : undefined,
        mediaMetadata,
      }),
      attachmentId,
    });
  },

  async uploadFiles(localFiles: LocalFile[], onProgress?: UploadProgressCallback): Promise<Attachment[]> {
    if (localFiles.length === 0) return [];

    const attachments: Attachment[] = [];

    for (const [fileIndex, localFile] of localFiles.entries()) {
      const { file } = localFile;
      const attachmentId = crypto.randomUUID();
      const attachmentName = `attachments/${attachmentId}`;
      reportProgress(onProgress, file.name, "preparing", 0, fileIndex, localFiles.length);
      const unsubscribe = subscribeAttachmentProgress(attachmentName, (event) => {
        reportProgress(
          onProgress,
          event.filename || file.name,
          event.stage as UploadProgressStage,
          event.progress,
          fileIndex,
          localFiles.length,
        );
      });

      reportProgress(onProgress, file.name, "uploading", 5, fileIndex, localFiles.length);
      try {
        const attachment = await uploadService.uploadFile(localFile, attachmentId);
        attachments.push(attachment);
        reportProgress(onProgress, attachment.filename, "complete", 100, fileIndex, localFiles.length);
      } finally {
        unsubscribe();
      }
    }

    return attachments;
  },
};
