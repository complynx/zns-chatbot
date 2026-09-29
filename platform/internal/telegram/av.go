package telegram

import "errors"

type AVKind string

const (
	AudioKind     AVKind = "audio"
	VoiceKind     AVKind = "voice"
	VideoKind     AVKind = "video"
	VideoNoteKind AVKind = "video_note"
)

type Audio struct {
	FileID    string     `json:"file_id"`
	UniqueID  string     `json:"file_unique_id"`
	Duration  int        `json:"duration"`
	Performer string     `json:"performer,omitempty"`
	Title     string     `json:"title,omitempty"`
	Filename  string     `json:"file_name,omitempty"`
	MIME      string     `json:"mime_type,omitempty"`
	Size      int64      `json:"file_size,omitempty"`
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
}

type Voice struct {
	FileID   string `json:"file_id"`
	UniqueID string `json:"file_unique_id"`
	Duration int    `json:"duration"`
	MIME     string `json:"mime_type,omitempty"`
	Size     int64  `json:"file_size,omitempty"`
}

type Video struct {
	FileID    string     `json:"file_id"`
	UniqueID  string     `json:"file_unique_id"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	Duration  int        `json:"duration"`
	Filename  string     `json:"file_name,omitempty"`
	MIME      string     `json:"mime_type,omitempty"`
	Size      int64      `json:"file_size,omitempty"`
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
}

type VideoNote struct {
	FileID    string     `json:"file_id"`
	UniqueID  string     `json:"file_unique_id"`
	Length    int        `json:"length"`
	Duration  int        `json:"duration"`
	Size      int64      `json:"file_size,omitempty"`
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
}

// AVAttachment preserves Telegram's field kind. Duration is only a metadata hint;
// the media worker validates actual decoded duration and content independently.
type AVAttachment struct {
	Kind     AVKind
	Document Document
	Duration int
}

var ErrAVAttachment = errors.New("invalid or ambiguous audio/video attachment")

// SelectAV returns only actual audio/voice/video/video_note fields. It does not
// infer kinds from documents, MIME labels, captions or sticker video flags.
// Multiple AV fields are malformed input and are rejected without guessing.
func SelectAV(message Message) (AVAttachment, bool, error) {
	var candidates []AVAttachment
	if audio := message.Audio; audio != nil {
		candidates = append(candidates, AVAttachment{
			Kind:     AudioKind,
			Duration: audio.Duration,
			Document: Document{
				FileID:   audio.FileID,
				UniqueID: audio.UniqueID,
				Filename: audio.Filename,
				MIME:     audio.MIME,
				Size:     audio.Size,
			},
		})
	}
	if voice := message.Voice; voice != nil {
		candidates = append(candidates, AVAttachment{Kind: VoiceKind, Duration: voice.Duration,
			Document: Document{FileID: voice.FileID, UniqueID: voice.UniqueID, MIME: voice.MIME, Size: voice.Size}})
	}
	if video := message.Video; video != nil {
		candidates = append(candidates, AVAttachment{
			Kind:     VideoKind,
			Duration: video.Duration,
			Document: Document{
				FileID:   video.FileID,
				UniqueID: video.UniqueID,
				Filename: video.Filename,
				MIME:     video.MIME,
				Size:     video.Size,
			},
		})
	}
	if note := message.VideoNote; note != nil {
		candidates = append(candidates, AVAttachment{Kind: VideoNoteKind, Duration: note.Duration,
			Document: Document{FileID: note.FileID, UniqueID: note.UniqueID, Size: note.Size}})
	}
	if len(candidates) == 0 {
		return AVAttachment{}, false, nil
	}
	if len(candidates) != 1 {
		return AVAttachment{}, true, ErrAVAttachment
	}
	attachment := candidates[0]
	if attachment.Document.FileID == "" || attachment.Document.Size < 0 ||
		attachment.Document.Size > MaxDocumentBytes ||
		attachment.Duration < 0 {
		return AVAttachment{}, true, ErrAVAttachment
	}
	return attachment, true, nil
}
