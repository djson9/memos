import { create } from "@bufbuild/protobuf";
import { attachmentServiceClient } from "@/connect";
import type { Attachment } from "@/types/proto/api/v1/attachment_service_pb";
import { AttachmentSchema, MotionMediaSchema } from "@/types/proto/api/v1/attachment_service_pb";
import { subscribeAttachmentProgress } from "@/utils/attachmentProgress";
import { generateUUID } from "@/utils/uuid";
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
  async uploadFiles(localFiles: LocalFile[], onProgress?: UploadProgressCallback): Promise<Attachment[]> {
    if (localFiles.length === 0) return [];

    const attachments: Attachment[] = [];

    for (const [fileIndex, localFile] of localFiles.entries()) {
      const { file, motionMedia } = localFile;
      const attachmentId = generateUUID();
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

      const buffer = new Uint8Array(await file.arrayBuffer());
      reportProgress(onProgress, file.name, "uploading", 5, fileIndex, localFiles.length);
      try {
        const attachment = await attachmentServiceClient.createAttachment({
          attachment: create(AttachmentSchema, {
            filename: file.name,
            size: BigInt(file.size),
            type: file.type,
            content: buffer,
            motionMedia: motionMedia ? create(MotionMediaSchema, motionMedia) : undefined,
          }),
          attachmentId,
        });
        attachments.push(attachment);
        reportProgress(onProgress, attachment.filename, "complete", 100, fileIndex, localFiles.length);
      } finally {
        unsubscribe();
      }
    }

    return attachments;
  },
};
