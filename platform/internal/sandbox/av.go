package sandbox

import (
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Fake metadata is deliberately independent of bytes, so QA can test false duration hints.
// Decoding and actual duration admission belong to the media worker.
func sandboxAV(message *telegram.Message, path, durationText string) error {
	if path == "/lab/document" || path == "/lab/photo" {
		return nil
	}
	duration := 0
	if durationText != "" {
		var err error
		duration, err = strconv.Atoi(durationText)
		if err != nil || duration < 0 || duration > 86400 {
			return telegram.ErrAVAttachment
		}
	}
	doc := message.Document
	switch path {
	case "/lab/audio":
		message.Audio = &telegram.Audio{FileID: doc.FileID, UniqueID: doc.UniqueID, Duration: duration,
			Filename: doc.Filename, MIME: doc.MIME, Size: doc.Size}
	case "/lab/voice":
		message.Voice = &telegram.Voice{FileID: doc.FileID, UniqueID: doc.UniqueID, Duration: duration,
			MIME: doc.MIME, Size: doc.Size}
	case "/lab/video":
		message.Video = &telegram.Video{FileID: doc.FileID, UniqueID: doc.UniqueID, Duration: duration,
			Filename: doc.Filename, MIME: doc.MIME, Size: doc.Size}
	case "/lab/video_note":
		message.VideoNote = &telegram.VideoNote{FileID: doc.FileID, UniqueID: doc.UniqueID,
			Duration: duration, Size: doc.Size}
	default:
		return telegram.ErrAVAttachment
	}
	message.Document = nil
	return nil
}
