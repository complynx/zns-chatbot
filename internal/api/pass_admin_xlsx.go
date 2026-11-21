package api

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

// The exporter writes registration and payment worksheets. Inline string cells keep arbitrary
// participant names literal, including names beginning with Excel formula characters.
func passAdminWorkbook(list db.PassAdminList) ([]byte, error) {
	var out bytes.Buffer
	archive := zip.NewWriter(&out)

	if err := writePassWorkbookParts(archive, "Registrations", "Payments"); err != nil {
		return nil, err
	}
	if err := writePassRegistrationSheet(archive, list); err != nil {
		return nil, err
	}
	if err := writePassPaymentSheet(archive, list.Payments); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Sheet names are application-defined constants.
func writePassWorkbookParts(archive *zip.Writer, sheetNames ...string) error {
	var types, sheets, relationships strings.Builder
	for i, name := range sheetNames {
		id := i + 1
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml"
ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, id)
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, name, id, id)
		fmt.Fprintf(&relationships, `<Relationship Id="rId%d"
Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
Target="worksheets/sheet%d.xml"/>`, id, id)
	}
	for _, part := range []struct{ path, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels"
ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml"
ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml"
ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
` + types.String() + `</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1"
Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
Target="xl/workbook.xml"/>
</Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets>` + sheets.String() + `</sheets>
</workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
` + relationships.String() + `</Relationships>`},
	} {
		partWriter, err := archive.Create(part.path)
		if err != nil {
			return err
		}
		if _, err = partWriter.Write([]byte(part.body)); err != nil {
			return err
		}
	}
	return nil
}

type passWorksheet struct {
	writer    io.Writer
	rowNumber int
}

func (sheet *passWorksheet) emit(values ...any) error {
	sheet.rowNumber++
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<row r="%d">`, sheet.rowNumber)
	for i, value := range values {
		ref := fmt.Sprintf("%c%d", 'A'+i, sheet.rowNumber)
		switch v := value.(type) {
		case int64:
			// Excel rounds integers longer than 15 digits; keep the original digits.
			if v >= -maxExcelInteger && v <= maxExcelInteger {
				fmt.Fprintf(&buf, `<c r="%s"><v>%d</v></c>`, ref, v)
				continue
			}
			value = strconv.FormatInt(v, 10)
		case *uuid.UUID:
			if v == nil {
				continue
			}
			value = v.String()
		case *time.Time:
			if v == nil {
				continue
			}
			value = v.UTC().Format(time.RFC3339Nano)
		case time.Time:
			value = v.UTC().Format(time.RFC3339Nano)
		}
		fmt.Fprintf(&buf, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">`, ref)
		if e := xml.EscapeText(&buf, []byte(fmt.Sprint(value))); e != nil {
			return e
		}
		buf.WriteString("</t></is></c>")
	}
	buf.WriteString("</row>")
	_, e := sheet.writer.Write(buf.Bytes())
	return e
}

const maxExcelInteger = 999_999_999_999_999

func writePassRegistrationSheet(archive *zip.Writer, list db.PassAdminList) error {
	writer, err := archive.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	if _, err = fmt.Fprint(
		writer,
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetViews>
<sheetView workbookViewId="0">
<pane ySplit="3" topLeftCell="A4" activePane="bottomLeft" state="frozen"/>
</sheetView>
</sheetViews>
<cols>
<col min="1" max="24" width="24" customWidth="1"/>
</cols>
<sheetData>`,
	); err != nil {
		return err
	}
	sheet := passWorksheet{writer: writer}
	if err = sheet.emit("event_id", list.EventID.String()); err != nil {
		return err
	}
	if err = sheet.emit("exported_at", list.AsOf); err != nil {
		return err
	}
	if err = sheet.emit(
		"registration_id",
		"telegram_id",
		"display_name",
		"state",
		"role",
		"requested_at",
		"role_queue_position",
		"effective_pair_priority_at",
		"partner_registration_id",
		"pass_id",
		"tier",
		"price_minor",
		"currency",
		"currency_scale",
		"confirmed_minor",
		"pending_proof",
		"refund_due_minor",
		"refunded_minor",
		"returned_at",
		"returned_by",
		"refunded_at",
		"refunded_by",
	); err != nil {
		return err
	}
	for _, r := range list.Rows {
		var position any = ""
		if r.RolePosition > 0 {
			position = r.RolePosition
		}
		if err = sheet.emit(
			r.RegistrationID.String(),
			strconv.FormatInt(r.TelegramID, 10),
			r.DisplayName,
			r.State,
			r.Role,
			r.RequestedAt,
			position,
			r.PairPriorityAt,
			r.PartnerRegistrationID,
			r.PassID,
			r.Tier,
			r.PriceMinor,
			r.Currency,
			int64(r.CurrencyScale),
			r.ConfirmedMinor,
			r.PendingProof,
			r.RefundDueMinor,
			r.RefundedMinor,
			r.ReturnedAt,
			r.ReturnedBy,
			r.RefundedAt,
			r.RefundedBy,
		); err != nil {
			return err
		}
	}
	if _, err = fmt.Fprintf(writer, `</sheetData><autoFilter ref="A3:V%d"/></worksheet>`, sheet.rowNumber); err != nil {
		return err
	}
	return nil
}

func writePassPaymentSheet(archive *zip.Writer, payments []db.PassAdminPayment) error {
	writer, err := archive.Create("xl/worksheets/sheet2.xml")
	if err != nil {
		return err
	}
	if _, err = fmt.Fprint(
		writer,
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<cols>
<col min="1" max="14" width="24" customWidth="1"/>
</cols>
<sheetData>`,
	); err != nil {
		return err
	}
	sheet := passWorksheet{writer: writer}
	if err = sheet.emit(
		"evidence_id",
		"registration_id",
		"pass_id",
		"purpose",
		"reported_minor",
		"currency",
		"currency_scale",
		"submitted_at",
		"decision",
		"confirmed_minor",
		"reviewed_at",
		"reviewer_id",
		"reviewer_name",
		"actual_receiver",
	); err != nil {
		return err
	}
	for _, p := range payments {
		// The evidence ledger identifies the reviewer. It does not establish who physically received money.
		if err = sheet.emit(
			p.EvidenceID,
			p.RegistrationID,
			p.PassID,
			p.Purpose,
			p.ReportedMinor,
			p.Currency,
			int64(p.CurrencyScale),
			p.SubmittedAt,
			p.Decision,
			p.ConfirmedMinor,
			p.ReviewedAt,
			p.ReviewedBy,
			p.ReviewerName,
			"",
		); err != nil {
			return err
		}
	}
	if _, err = fmt.Fprintf(writer, `</sheetData><autoFilter ref="A1:N%d"/></worksheet>`, sheet.rowNumber); err != nil {
		return err
	}
	return nil
}
