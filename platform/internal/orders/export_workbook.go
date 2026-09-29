package orders

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/xuri/excelize/v2"
)

const exportOrdersSheet = "Заказы"
const exportDetailsSheet = "Содержимое"
const maxExportCells = 1000000
const exportColumnWidth = 24

type exportWorkbook struct {
	file   *excelize.File
	rows   map[string]int
	widths map[string]int
	cells  int
	header int
}

func renderExport(ctx context.Context, catalog Catalog, list []exportOrder) ([]byte, error) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }() // Workbook is in memory; discard on any failure.
	if err := file.SetSheetName("Sheet1", exportOrdersSheet); err != nil {
		return nil, err
	}
	style, err := file.NewStyle(
		&excelize.Style{Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{Horizontal: "center"}},
	)
	if err != nil {
		return nil, err
	}
	w := &exportWorkbook{file: file, rows: map[string]int{}, widths: map[string]int{}, header: style}
	report := newExportReport(catalog)
	if err = report.headers(w); err != nil {
		return nil, err
	}
	for _, entry := range list {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if err = report.order(w, entry); err != nil {
			return nil, err
		}
	}
	if err = report.totals(w); err != nil {
		return nil, err
	}
	if err = w.finish(); err != nil {
		return nil, err
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	if buffer.Len() > MaxExportBytes {
		return nil, problem(http.StatusRequestEntityTooLarge, "export_too_large")
	}
	return buffer.Bytes(), nil
}

func (w *exportWorkbook) append(sheet string, values []any) error {
	if w.cells+len(values) > maxExportCells {
		return problem(http.StatusRequestEntityTooLarge, "export_too_large")
	}
	for index, value := range values {
		if text, ok := value.(string); ok {
			text = exportText(text)
			units := 0
			for _, char := range text {
				units += utf16.RuneLen(char)
			}
			if units > excelize.TotalCellChars {
				return problem(http.StatusRequestEntityTooLarge, "export_too_large")
			}
			values[index] = text
		}
	}
	w.rows[sheet]++
	w.cells += len(values)
	cell, err := excelize.CoordinatesToCellName(1, w.rows[sheet])
	if err != nil {
		return err
	}
	return w.file.SetSheetRow(sheet, cell, &values)
}

func (w *exportWorkbook) sheet(name string, headers []any) error {
	w.widths[name] = len(headers)
	if name != exportOrdersSheet {
		if _, err := w.file.NewSheet(name); err != nil {
			return err
		}
	}
	if err := w.append(name, headers); err != nil {
		return err
	}
	last, err := excelize.CoordinatesToCellName(len(headers), 1)
	if err != nil {
		return err
	}
	if err = w.file.SetCellStyle(name, "A1", last, w.header); err != nil {
		return err
	}
	column, err := excelize.ColumnNumberToName(len(headers))
	if err != nil {
		return err
	}
	return w.file.SetColWidth(name, "A", column, exportColumnWidth)
}

func (w *exportWorkbook) finish() error {
	const decimalMoneyFormat = 2
	moneyStyle, styleError := w.file.NewStyle(&excelize.Style{NumFmt: decimalMoneyFormat})
	if styleError != nil {
		return styleError
	}
	for _, sheet := range w.file.GetSheetList() {
		if err := w.file.SetPanes(
			sheet,
			&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"},
		); err != nil {
			return err
		}
		last, err := excelize.CoordinatesToCellName(w.widths[sheet], w.rows[sheet])
		if err != nil {
			return err
		}
		if err = w.file.AutoFilter(sheet, "A1:"+last, nil); err != nil {
			return err
		}
		if err = w.moneyStyle(sheet, moneyStyle); err != nil {
			return err
		}
	}
	return nil
}

func (w *exportWorkbook) moneyStyle(sheet string, style int) error {
	if w.rows[sheet] <= 1 {
		return nil
	}
	var firstColumn, lastColumn string
	switch sheet {
	case exportOrdersSheet:
		firstColumn, lastColumn = "J", "K"
	case "Итоги", "Посуда":
		firstColumn, lastColumn = "C", "C"
	default:
		return nil
	}
	return w.file.SetCellStyle(sheet, firstColumn+"2", lastColumn+strconv.Itoa(w.rows[sheet]), style)
}

// Text cells never become formulas. Remove XML-invalid characters from legacy data.
func exportText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 32 && r != 0xfffe && r != 0xffff) {
			return r
		}
		return -1
	}, value)
}
