package passbooking

import (
	"context"
	"unicode/utf16"

	"github.com/xuri/excelize/v2"
)

const exportSheet = "Passes"
const exportColumns = 25
const exportFirstDataRow = 2

func passExportHeaders() []any {
	return []any{"User ID", "Username", "First Name", "Last Name", "Print Name", "Legal Name", "Language code",
		"Pass Key", "State", "Type", "Role", "Couple", "Price", "Price per one", "Assignment Tier", "Date Created",
		"Date Assignment", "Current Ambassador", "Proven By", "Proof Received", "Proof Accepted", "Proof Rejected",
		"Proof File ID", "Skip in Balance Count", "Comment"}
}

func renderPassExport(ctx context.Context, rows [][]any) ([]byte, error) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	if err := file.SetSheetName("Sheet1", exportSheet); err != nil {
		return nil, err
	}
	headers := passExportHeaders()
	if err := file.SetSheetRow(exportSheet, "A1", &headers); err != nil {
		return nil, err
	}
	for index, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := exportCellsValid(row); err != nil {
			return nil, err
		}
		cell, err := excelize.CoordinatesToCellName(1, index+exportFirstDataRow)
		if err != nil {
			return nil, err
		}
		// SetSheetRow writes strings as string cells; untrusted '=' prefixes are never formulas.
		if err = file.SetSheetRow(exportSheet, cell, &row); err != nil {
			return nil, err
		}
	}
	if err := finishPassExport(file, len(rows)+1); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	if buffer.Len() > MaxExportBytes {
		return nil, exportTooLarge()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func exportCellsValid(row []any) error {
	for _, value := range row {
		text, ok := value.(string)
		if !ok {
			continue
		}
		units := 0
		for _, char := range text {
			if (char < 32 && char != '\t' && char != '\n' && char != '\r') || char == 0xfffe || char == 0xffff {
				return invalid()
			}
			units += utf16.RuneLen(char)
		}
		if units > excelize.TotalCellChars {
			return exportTooLarge()
		}
	}
	return nil
}

func finishPassExport(file *excelize.File, rows int) error {
	style, err := file.NewStyle(
		&excelize.Style{Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{Horizontal: "center"}},
	)
	if err != nil {
		return err
	}
	if err = file.SetCellStyle(exportSheet, "A1", "Y1", style); err != nil {
		return err
	}
	const width = 24
	if err = file.SetColWidth(exportSheet, "A", "Y", width); err != nil {
		return err
	}
	if err = file.SetPanes(
		exportSheet,
		&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"},
	); err != nil {
		return err
	}
	last, err := excelize.CoordinatesToCellName(exportColumns, rows)
	if err != nil {
		return err
	}
	return file.AutoFilter(exportSheet, "A1:"+last, nil)
}
