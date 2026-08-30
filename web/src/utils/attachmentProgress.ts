export interface AttachmentProgressEvent {
  name: string;
  filename: string;
  stage: string;
  progress: number;
}

type AttachmentProgressListener = (event: AttachmentProgressEvent) => void;

const listeners = new Map<string, Set<AttachmentProgressListener>>();

export function subscribeAttachmentProgress(name: string, listener: AttachmentProgressListener): () => void {
  const resourceListeners = listeners.get(name) ?? new Set<AttachmentProgressListener>();
  resourceListeners.add(listener);
  listeners.set(name, resourceListeners);

  return () => {
    resourceListeners.delete(listener);
    if (resourceListeners.size === 0) {
      listeners.delete(name);
    }
  };
}

export function publishAttachmentProgress(event: AttachmentProgressEvent): void {
  const resourceListeners = listeners.get(event.name);
  if (!resourceListeners) return;
  for (const listener of resourceListeners) {
    listener(event);
  }
}
