package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

const passIdentityExportTimeout = 5 * time.Minute

func (s *Server) passIdentityExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Pragma", "no-cache")
	if !s.miniAppRequest(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	serveBoundedAPI(w, r, http.HandlerFunc(s.downloadPassIdentity), passIdentityExportTimeout)
}

func (s *Server) downloadPassIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	event, err := miniAppPairID(r.PathValue("event"))
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	scope, session, err := s.miniAppPairAssignmentSession(ctx, r)
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	err = s.buildPassIdentity(
		ctx,
		scope,
		&session,
		event,
		func(policy db.PassIdentityPolicy, archive io.Reader, size int64) error {
			refresh := func(ctx context.Context) (bool, error) {
				principal, authErr := s.verifyMiniAppSubject(ctx, scope.Subject, s.iam.VerifyFresh)
				if authErr != nil {
					return false, authErr
				}
				_, global := principal.Roles["zinusya.admin"][scope.Organization]
				return global, nil
			}
			if authErr := s.store.CheckPassIdentityExport(ctx, scope, session, event, policy, refresh); authErr != nil {
				return authErr
			}
			w.Header().Set("Content-Disposition", `attachment; filename="identity-`+event.String()+`.zip"`)
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			if _, copyErr := io.CopyN(w, archive, size); copyErr != nil {
				// Headers are committed. Close the connection so the short download fails.
				panic(http.ErrAbortHandler)
			}
			return nil
		},
	)
	if err != nil {
		miniAppPairAssignmentError(w, err)
	}
}

func (s *Server) buildPassIdentity(ctx context.Context, scope db.PaymentScope, session *db.MiniAppPairAssignmentSession,
	event uuid.UUID, release func(db.PassIdentityPolicy, io.Reader, int64) error,
) error {
	var policy db.PassIdentityPolicy
	return withPassIdentityArchive(func(out io.Writer) error {
		book := passIdentityWorkbook{archive: zip.NewWriter(out)}
		var readErr error
		policy, readErr = s.store.StreamPassIdentityExport(
			ctx, scope, session, event, s.options.RegistrationDetails, book.begin, book.add,
		)
		if readErr != nil {
			return readErr
		}
		return book.finish()
	}, func(archive io.Reader, size int64) error { return release(policy, archive, size) })
}

// Only ciphertext goes to the temporary file. Its key and expected digest
// live in this request. Verify the completed file before authorizing release.
func withPassIdentityArchive(build func(io.Writer) error, release func(io.Reader, int64) error) error {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "shuttle-export-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	// Each archive has a new key, so the initial counter is unique per key.
	var counter [aes.BlockSize]byte
	expected := sha256.New()
	encrypted := cipher.StreamWriter{S: cipher.NewCTR(block, counter[:]), W: io.MultiWriter(file, expected)}
	if err = build(&encrypted); err != nil {
		return err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	actual := sha256.New()
	size, err := io.Copy(actual, file)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected.Sum(nil), actual.Sum(nil)) {
		return errors.New("identity export verification failed")
	}
	encryptedFile := io.NewSectionReader(file, 0, size)
	return release(&cipher.StreamReader{S: cipher.NewCTR(block, counter[:]), R: encryptedFile}, size)
}

// Photos go directly into the outer archive. Only the compressed worksheet
// and the two ZIP directories accumulate while the database streams rows.
type passIdentityWorkbook struct {
	archive *zip.Writer
	data    bytes.Buffer
	xlsx    *zip.Writer
	sheet   passWorksheet
	fields  registrationdetails.Requirements
}

func (book *passIdentityWorkbook) begin(policy db.PassIdentityPolicy) error {
	book.fields = policy.Requirements
	book.xlsx = zip.NewWriter(&book.data)
	if err := writePassWorkbookParts(book.xlsx, "Participants"); err != nil {
		return err
	}
	part, err := book.xlsx.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	book.sheet.writer = part
	if _, err = io.WriteString(part, `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" state="frozen"/></sheetView></sheetViews>
<cols><col min="1" max="6" width="32" customWidth="1"/></cols><sheetData>`); err != nil {
		return err
	}
	return book.emit(
		"registration_id", "legal_name", "passport_number", "passport_country", "country_category", "passport_photo",
	)
}

func (book *passIdentityWorkbook) emit(id, name, number, country, category, photo string) error {
	values := []any{id}
	if book.fields.LegalName {
		values = append(values, name)
	}
	if book.fields.PassportNumber {
		values = append(values, number)
	}
	if len(book.fields.PassportCountries) != 0 {
		values = append(values, country, category)
	}
	if book.fields.PassportPhoto {
		values = append(values, photo)
	}
	return book.sheet.emit(values...)
}

func (book *passIdentityWorkbook) add(row db.PassIdentityRow) error {
	// An XLSX worksheet can contain up to 1,048,576 rows, including its header.
	if book.sheet.rowNumber >= 1<<20 {
		return errors.New("identity export exceeds the worksheet row limit")
	}
	id := row.RegistrationID.String()
	var path string
	if book.fields.PassportPhoto && row.PassportPhoto != "" {
		var err error
		path, err = book.addPhoto(id, row.PassportPhoto)
		if err != nil {
			return err
		}
	}
	return book.emit(id, row.LegalName, row.PassportNumber, row.PassportCountry, row.CountryCategory, path)
}

func (book *passIdentityWorkbook) addPhoto(id, encoded string) (string, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(registrationdetails.MaxPhotoBytes) {
		return "", registrationdetails.ErrProtection
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > registrationdetails.MaxPhotoBytes {
		return "", registrationdetails.ErrProtection
	}
	path := "photos/" + id
	switch http.DetectContentType(data) {
	case "image/jpeg":
		path += ".jpg"
	case "image/png":
		path += ".png"
	default:
		return "", registrationdetails.ErrProtection
	}
	entry, err := book.archive.CreateHeader(&zip.FileHeader{Name: path, Method: zip.Store})
	if err != nil {
		return "", err
	}
	_, err = entry.Write(data)
	return path, err
}

func (book *passIdentityWorkbook) finish() error {
	if _, err := io.WriteString(book.sheet.writer, "</sheetData></worksheet>"); err != nil {
		return err
	}
	if err := book.xlsx.Close(); err != nil {
		return err
	}
	part, err := book.archive.CreateHeader(&zip.FileHeader{Name: "participants.xlsx", Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err = book.data.WriteTo(part); err != nil {
		return err
	}
	return book.archive.Close()
}
