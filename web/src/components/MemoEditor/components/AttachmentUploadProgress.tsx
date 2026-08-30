import type { FC } from "react";
import { useEditorSelector } from "../state";

const stageLabels: Record<string, string> = {
  preparing: "Preparing",
  uploading: "Uploading",
  received: "Upload received",
  queued: "Waiting to compress",
  compressing: "Compressing",
  saving: "Saving",
  complete: "Complete",
};

export const AttachmentUploadProgress: FC = () => {
  const progress = useEditorSelector((state) => state.uploadProgress);
  if (!progress) return null;

  const stageLabel = stageLabels[progress.stage] ?? "Processing";
  const fileCounter = progress.totalFiles > 1 ? ` · ${progress.currentFile}/${progress.totalFiles}` : "";

  return (
    <div className="w-full rounded-md border border-border bg-muted/40 px-3 py-2" aria-live="polite">
      <div className="mb-1.5 flex items-center justify-between gap-3 text-xs">
        <span className="min-w-0 truncate text-muted-foreground">
          {stageLabel} {progress.filename}
          {fileCounter}
        </span>
        <span className="shrink-0 tabular-nums text-foreground">{progress.overallPercent}%</span>
      </div>
      <div
        className="h-1.5 overflow-hidden rounded-full bg-foreground/10"
        role="progressbar"
        aria-label={`${stageLabel} ${progress.filename}`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={progress.overallPercent}
      >
        <div
          className="h-full rounded-full bg-primary transition-[width] duration-300 ease-out"
          style={{ width: `${progress.overallPercent}%` }}
        />
      </div>
    </div>
  );
};
