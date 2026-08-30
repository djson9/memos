# Media compression

This fork can compress website image and video attachments before they are stored. Compression is disabled unless explicitly enabled.

## Configuration

| Flag | Environment variable | Default | Purpose |
| --- | --- | ---: | --- |
| `--media-compression` | `MEMOS_MEDIA_COMPRESSION` | `false` | Enable image and video compression. |
| `--media-compression-max-input-mb` | `MEMOS_MEDIA_COMPRESSION_MAX_INPUT_MB` | `200` | Largest source media file accepted for compression. |
| `--media-compression-cpu-limit` | `MEMOS_MEDIA_COMPRESSION_CPU_LIMIT` | `150` | Compressor CPU budget as a percentage of one core. |

The runtime image includes libvips for images, FFmpeg for videos, and `cpulimit` for compressor-only CPU throttling. Compression jobs are serialized, so the configured CPU budget is the maximum aggregate compression load. Memos request handling and the database remain outside that throttle.

## Output policy

- Images are resized to fit within 2560×2560. JPEG and WebP use quality 82; PNG uses lossless level-9 compression.
- HEIC, HEIF, and TIFF images are converted to JPEG.
- Videos are resized to fit within 1920×1920 and converted to H.264/AAC MP4 with CRF 24.
- Apple Live Photos and Android Motion Photos are kept unchanged to preserve their pairing metadata.
- A transformed file is retained only if it saves at least 5% and 256 KiB. Otherwise, the original is stored.
- The normal instance upload limit is checked against the compressed result. Oversized source media may therefore succeed when its compressed result fits.

The memo editor displays upload, queue, compression, and save progress. Video progress is based on FFmpeg's processed timestamp; image progress reflects the native compressor phases.
