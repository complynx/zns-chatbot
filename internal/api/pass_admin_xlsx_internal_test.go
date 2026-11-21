package api

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestPassAdminWorkbookPreservesLiteralNamesAndLargeMoney(t *testing.T) {
	t.Parallel()
	const name = "=A1&<text>"
	amount := int64(9007199254740991)
	event := uuid.New()
	report := db.PassAdminList{EventID: event, AsOf: time.Now(), Rows: []db.PassAdminRow{
		{RegistrationID: uuid.New(), DisplayName: name, Currency: "EUR", CurrencyScale: 2, PriceMinor: amount},
	}}
	data, err := passAdminWorkbook(report)
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	file, err := archive.Open("xl/worksheets/sheet1.xml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	sheet, err := io.ReadAll(file)
	require.NoError(t, err)
	var parsed struct {
		Rows []struct {
			Cells []struct {
				Ref     string  `xml:"r,attr"`
				Type    string  `xml:"t,attr"`
				Text    string  `xml:"is>t"`
				Formula *string `xml:"f"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	require.NoError(t, xml.Unmarshal(sheet, &parsed))
	require.Len(t, parsed.Rows, 4)
	cells := map[string]string{}
	for _, cell := range parsed.Rows[3].Cells {
		require.Nil(t, cell.Formula)
		if cell.Type == "inlineStr" {
			cells[cell.Ref] = cell.Text
		}
	}
	require.Equal(t, name, cells["C4"])
	require.Equal(t, "9007199254740991", cells["L4"])
}
