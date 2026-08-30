import { describe, expect, it, vi } from "vitest";
import { publishAttachmentProgress, subscribeAttachmentProgress } from "./attachmentProgress";

describe("attachment progress", () => {
  it("delivers events only to listeners for the matching attachment", () => {
    const matchingListener = vi.fn();
    const otherListener = vi.fn();
    const unsubscribeMatching = subscribeAttachmentProgress("attachments/one", matchingListener);
    const unsubscribeOther = subscribeAttachmentProgress("attachments/two", otherListener);

    publishAttachmentProgress({
      name: "attachments/one",
      filename: "photo.jpg",
      stage: "compressing",
      progress: 42,
    });

    expect(matchingListener).toHaveBeenCalledOnce();
    expect(matchingListener).toHaveBeenCalledWith({
      name: "attachments/one",
      filename: "photo.jpg",
      stage: "compressing",
      progress: 42,
    });
    expect(otherListener).not.toHaveBeenCalled();

    unsubscribeMatching();
    unsubscribeOther();
  });

  it("stops delivery after unsubscribe", () => {
    const listener = vi.fn();
    const unsubscribe = subscribeAttachmentProgress("attachments/one", listener);
    unsubscribe();

    publishAttachmentProgress({
      name: "attachments/one",
      filename: "clip.mp4",
      stage: "complete",
      progress: 100,
    });

    expect(listener).not.toHaveBeenCalled();
  });
});
