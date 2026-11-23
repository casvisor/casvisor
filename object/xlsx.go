// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"bytes"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

const XlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

const maxXlsxUnzipSize = 256 << 20

// XlsxStream writes the rows of one sheet as they come, so that a large export does not stay in memory.
// Every string is written as a string cell, which a spreadsheet never runs as a formula.
type XlsxStream struct {
	file   *excelize.File
	writer *excelize.StreamWriter
	row    int
}

func NewXlsxStream(sheet string, header []string) (*XlsxStream, error) {
	file := excelize.NewFile()
	err := file.SetSheetName("Sheet1", sheet)
	if err != nil {
		return nil, err
	}
	writer, err := file.NewStreamWriter(sheet)
	if err != nil {
		return nil, err
	}
	stream := &XlsxStream{file: file, writer: writer}

	style, err := file.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	err = writer.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	if err != nil {
		return nil, err
	}
	err = writer.SetColWidth(1, max(len(header), 1), 20)
	if err != nil {
		return nil, err
	}
	cells := []interface{}{}
	for _, value := range header {
		cells = append(cells, excelize.Cell{StyleID: style, Value: value})
	}
	return stream, stream.writeRow(cells)
}

func (stream *XlsxStream) writeRow(cells []interface{}) error {
	stream.row++
	if stream.row > excelize.TotalRows {
		return fmt.Errorf("a sheet can not have more than %d rows", excelize.TotalRows)
	}
	cell, err := excelize.CoordinatesToCellName(1, stream.row)
	if err != nil {
		return err
	}
	return stream.writer.SetRow(cell, cells)
}

func (stream *XlsxStream) WriteRow(row []string) error {
	cells := make([]interface{}, len(row))
	for i, value := range row {
		cells[i] = value
	}
	return stream.writeRow(cells)
}

func (stream *XlsxStream) Close(w io.Writer) error {
	defer stream.file.Close()
	err := stream.writer.Flush()
	if err != nil {
		return err
	}
	_, err = stream.file.WriteTo(w)
	return err
}

const (
	xlsxRowPlain = iota
	xlsxRowHeading
	xlsxRowHeader
)

type xlsxRow struct {
	kind  int
	cells []interface{}
}

// XlsxSheet is a sheet of a workbook, whose cells are strings, numbers or nil for empty ones.
type XlsxSheet struct {
	Name string
	rows []*xlsxRow
}

func (sheet *XlsxSheet) Heading(title string) {
	sheet.rows = append(sheet.rows, &xlsxRow{kind: xlsxRowHeading, cells: []interface{}{title}})
}

func (sheet *XlsxSheet) Header(cells ...string) {
	row := &xlsxRow{kind: xlsxRowHeader}
	for _, cell := range cells {
		row.cells = append(row.cells, cell)
	}
	sheet.rows = append(sheet.rows, row)
}

func (sheet *XlsxSheet) Add(cells ...interface{}) {
	sheet.rows = append(sheet.rows, &xlsxRow{cells: cells})
}

func WriteXlsx(sheets []*XlsxSheet) ([]byte, error) {
	file := excelize.NewFile()
	defer file.Close()

	styles := map[int]int{}
	for kind, font := range map[int]*excelize.Font{xlsxRowHeading: {Bold: true, Size: 13}, xlsxRowHeader: {Bold: true}} {
		style, err := file.NewStyle(&excelize.Style{Font: font})
		if err != nil {
			return nil, err
		}
		styles[kind] = style
	}

	for i, sheet := range sheets {
		var err error
		if i == 0 {
			err = file.SetSheetName("Sheet1", sheet.Name)
		} else {
			_, err = file.NewSheet(sheet.Name)
		}
		if err != nil {
			return nil, err
		}
		writer, err := file.NewStreamWriter(sheet.Name)
		if err != nil {
			return nil, err
		}
		columns := 1
		for _, row := range sheet.rows {
			columns = max(columns, len(row.cells))
		}
		err = writer.SetColWidth(1, 1, 36)
		if err != nil {
			return nil, err
		}
		if columns > 1 {
			err = writer.SetColWidth(2, columns, 18)
			if err != nil {
				return nil, err
			}
		}

		for j, row := range sheet.rows {
			cells := make([]interface{}, len(row.cells))
			for k, value := range row.cells {
				if style, ok := styles[row.kind]; ok {
					cells[k] = excelize.Cell{StyleID: style, Value: value}
				} else {
					cells[k] = value
				}
			}
			cell, err := excelize.CoordinatesToCellName(1, j+1)
			if err != nil {
				return nil, err
			}
			err = writer.SetRow(cell, cells)
			if err != nil {
				return nil, err
			}
		}
		err = writer.Flush()
		if err != nil {
			return nil, err
		}
	}

	buf := &bytes.Buffer{}
	_, err := file.WriteTo(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadXlsxRows reads the cells of the first sheet of a workbook as text.
func ReadXlsxRows(data []byte) ([][]string, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: maxXlsxUnzipSize, UnzipXMLSizeLimit: 64 << 20})
	if err != nil {
		return nil, fmt.Errorf("the file is not an Excel workbook (.xlsx): %s", err.Error())
	}
	defer file.Close()

	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("the workbook has no sheet")
	}
	return file.GetRows(sheets[0])
}
