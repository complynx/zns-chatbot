package integration_test

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func (p *peers) sendDocument(w http.ResponseWriter, r *http.Request) {
	p.documentAttempts = append(p.documentAttempts, time.Now())
	if p.documentRejectOnce != 0 {
		status := p.documentRejectOnce
		p.documentRejectOnce = 0
		w.WriteHeader(status)
		respond(w, map[string]any{"ok": false, "error_code": status, "parameters": map[string]int{"retry_after": 2}})
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("document")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	chat, err := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	p.documentSent = append(
		p.documentSent,
		telegram.SendDocumentRequest{ChatID: chat, Filename: header.Filename, Content: content},
	)
	if p.documentEntered != nil {
		close(p.documentEntered)
		p.documentEntered = nil
		select {
		case <-p.documentRelease:
		case <-r.Context().Done():
			return
		}
	}
	if p.documentWrongReceipt {
		p.documentWrongReceipt = false
		chat++
	}
	if p.loseDocumentSend {
		p.loseDocumentSend = false
		disconnect(w)
		return
	}
	respond(w, map[string]any{"ok": true, "result": map[string]any{
		"message_id": len(p.sent) + len(p.documentSent), "chat": map[string]int64{"id": chat},
	}})
}
