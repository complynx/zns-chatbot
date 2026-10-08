package sandbox

import (
	"crypto/rand"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type documentUpload struct {
	chat int64
	name string
	body []byte
}

const multipartOverheadBytes = 64 << 10

func (upload *documentUpload) readPart(part *multipart.Part) error {
	limit := int64(telegram.MaxDocumentBytes)
	if part.FormName() == "chat_id" {
		limit = 32
	}
	body, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err != nil {
		return err
	}
	if len(body) == 0 || int64(len(body)) > limit {
		return errors.New("invalid field size")
	}
	if part.FormName() == "document" {
		upload.name = part.FileName()
		upload.body = body
		return nil
	}
	upload.chat, err = strconv.ParseInt(string(body), 10, 64)
	return err
}

func parseDocument(w http.ResponseWriter, r *http.Request) (documentUpload, error) {
	r.Body = http.MaxBytesReader(w, r.Body, telegram.MaxDocumentBytes+multipartOverheadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return documentUpload{}, err
	}
	var upload documentUpload
	seen := map[string]bool{}
	for {
		part, partError := reader.NextPart()
		if errors.Is(partError, io.EOF) {
			break
		}
		if partError != nil {
			return upload, partError
		}
		name := part.FormName()
		if seen[name] || (name != "chat_id" && name != "document") {
			return upload, errors.New("invalid form field")
		}
		seen[name] = true
		if err = upload.readPart(part); err != nil {
			return upload, err
		}
	}
	if upload.chat == 0 || upload.name == "" || len(upload.name) > 255 || len(upload.body) == 0 {
		return upload, errors.New("missing document")
	}
	return upload, nil
}

func (f *Fake) sendDocument(w http.ResponseWriter, r *http.Request) {
	upload, err := parseDocument(w, r)
	if err != nil {
		tgError(w, http.StatusBadRequest, "invalid document")
		return
	}
	chat, known := f.deliveryDestination(strconv.FormatInt(upload.chat, 10))
	if !known {
		tgError(w, http.StatusBadRequest, "chat not found")
		return
	}
	if f.DB == nil {
		tgError(w, http.StatusBadRequest, "invalid document")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectProviderFault(w, r, sendDocumentMethod, upload.chat, 0) {
		return
	}
	if f.blocked[upload.chat] {
		tgError(w, http.StatusForbidden, "bot was blocked by the user")
		return
	}
	if f.fault == transientFault {
		f.fault = noFault
		tgError(w, http.StatusTooManyRequests, "Too Many Requests")
		return
	}
	id := rand.Text()
	_, err = f.DB.Exec(
		r.Context(),
		`INSERT INTO bot.fake_files(id,filename,body) VALUES($1,$2,$3)`,
		id,
		upload.name,
		upload.body,
	)
	if err != nil {
		tgError(w, http.StatusServiceUnavailable, "file storage unavailable")
		return
	}
	f.next++
	message := telegram.Message{
		ID:   f.next,
		Chat: chat,
		From: telegram.User{ID: fakeBotID, IsBot: true},
		Document: &telegram.Document{
			FileID:   id,
			UniqueID: id,
			Filename: upload.name,
			Size:     int64(len(upload.body)),
			MIME:     "application/octet-stream",
		},
	}
	f.messages = append(f.messages, message)
	if err = f.save(r.Context()); err != nil {
		tgError(w, http.StatusServiceUnavailable, "state unavailable")
		return
	}
	tgOK(w, message)
}
