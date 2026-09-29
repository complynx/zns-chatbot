package sandbox

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/api"
)

//go:embed asr_fixtures.json
var asrFixtures []byte

const fixtureASRKey = "sandbox-only-synthetic-asr"
const maxFixtureWAV = 8 << 20
const multipartOverhead = 4096
const pcmBits = 16
const waveHeaderBytes = 12
const waveAlignment = 2

// Only exact synthetic PCM fixtures receive saved transcripts. Unknown speech is
// rejected; the stand never pretends to recognize arbitrary user recordings.
func (f *Fake) fixtureTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+fixtureASRKey {
		api.JSON(w, http.StatusUnauthorized, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFixtureWAV+multipartOverhead)
	wav, model := fixtureASRBody(r)
	pcm := fixturePCM(wav)
	if pcm == nil || model != "gpt-transcribe" {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	digest := sha256.Sum256(pcm)
	var fixtures map[string]string
	if err := json.Unmarshal(asrFixtures, &fixtures); err != nil {
		api.JSON(w, http.StatusInternalServerError, nil)
		return
	}
	transcript, known := fixtures[hex.EncodeToString(digest[:])]
	if !known {
		api.JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "unknown synthetic fixture"})
		return
	}
	f.mu.Lock()
	f.asrCalls++
	f.mu.Unlock()
	api.JSON(w, http.StatusOK, map[string]string{"text": transcript})
}

func fixtureASRBody(r *http.Request) ([]byte, string) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, ""
	}
	var wav []byte
	model := ""
	for range 2 {
		part, partErr := reader.NextPart()
		if partErr != nil {
			return nil, ""
		}
		body, readErr := io.ReadAll(io.LimitReader(part, maxFixtureWAV+1))
		if readErr != nil || len(body) > maxFixtureWAV {
			return nil, ""
		}
		switch part.FormName() {
		case "file":
			if wav != nil {
				return nil, ""
			}
			wav = body
		case "model":
			if model != "" || len(body) > 128 {
				return nil, ""
			}
			model = string(body)
		default:
			return nil, ""
		}
	}
	if _, err = reader.NextPart(); !errors.Is(err, io.EOF) {
		return nil, ""
	}
	return wav, model
}

// FFmpeg's pipe WAV uses an unknown data length; accept only the bounded remainder.
func fixturePCM(wav []byte) []byte {
	if len(wav) < waveHeaderBytes || !bytes.Equal(wav[:4], []byte("RIFF")) || !bytes.Equal(wav[8:12], []byte("WAVE")) {
		return nil
	}
	validFormat := false
	length := int64(len(wav))
	for offset := int64(waveHeaderBytes); offset+8 <= length; {
		kind := string(wav[offset : offset+4])
		size := int64(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		offset += 8
		if kind == "data" && validFormat && size == 0xffffffff {
			size = length - offset
		}
		if size > length-offset {
			return nil
		}
		chunk := wav[offset : offset+size]
		if kind == "fmt " && len(chunk) >= 16 {
			validFormat = binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 1 &&
				binary.LittleEndian.Uint32(chunk[4:]) == 16000 && binary.LittleEndian.Uint16(chunk[14:]) == pcmBits
		}
		if kind == "data" {
			if validFormat && len(chunk) > 0 && len(chunk)%2 == 0 {
				return chunk
			}
			return nil
		}
		offset += size + size%waveAlignment
	}
	return nil
}

func (f *Fake) fixtureASRState(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	api.JSON(w, http.StatusOK, map[string]int{"recognized_fixture_calls": f.asrCalls})
}
