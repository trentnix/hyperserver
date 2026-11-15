# Audio

Use **Add component → Audio** below the Markdown editor. Upload one MP3 or choose one from the library. The title, description, and transcript are optional. Save the post to apply changes.

Readers get native playback controls with autoplay disabled and `preload="none"`. The transcript expands below the player and supports Markdown. Raw HTML and unsafe links are omitted. **Show download link** controls the separate Download MP3 link. Hiding the link does not prevent downloading the recording.

**Replace MP3** and **Library** replace the recording while preserving its title, description, transcript, and download setting. Undo restores the previous recording. Component ordering, removal, Undo, unsaved indicators, browser recovery, and revision history work as they do for other components. An audio-only post is allowed. Page editing does not expose audio controls yet.

## Limits and storage

Each component accepts one MP3 of at most 32 MiB. Titles allow 240 characters, descriptions 1,000, and transcripts 20,000. MP3s share the site's 1 GiB [file storage](downloads.md). Upload validation checks two complete MPEG Layer III frames after an optional ID3v2 tag. Standard CBR and VBR bitrates are supported. Free-format bitrates are not supported. The application does not decode or transcode recordings.

The existing version 2 `post-components` block stores an `audio` record with an `audio` object. The object holds the immutable file `id`, byte `size`, optional `title`, `description`, `transcript`, and `hideDownload`. No database migration is required.

Playback uses the same authorization, byte ranges, cache validation, cleanup, and backups as file downloads. Drafts, scheduled posts before publication, and historical references do not grant public access. Hiding the download link still retains the playback reference. Feeds provide an absolute listening link. Search includes authored details and transcript text.

Format references: [MP3 frame headers](https://samples.ffmpeg.org/A-codecs/sf/mpeg_header.html) and [ID3v2.4 structure](https://id3.org/id3v2.4.0-structure).
