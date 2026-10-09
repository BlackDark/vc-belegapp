package export

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// CSVHeader is the specification header, without the trailing line break.
const CSVHeader = "nr;datum;wochentag;mahlzeit;bezugsort;arbeitsort;haendler;ort;belegbetrag;anerkannt;korrektur_grund;erstattung;eigenanteil;geldwerter_vorteil;steuerfrei;regulaer;warnungen;bild_sha256"

// Row is one receipt in the CSV. Amounts are integer cents.
type Row struct {
	Nr             int
	Datum          string
	Wochentag      string
	Mahlzeit       string
	Bezugsort      string
	Arbeitsort     string
	Haendler       string
	Ort            string
	Belegbetrag    int
	Anerkannt      int
	KorrekturGrund string
	Erstattung     int
	Eigenanteil    int
	GV             int
	Steuerfrei     int
	Regulaer       int
	Warnungen      string
	BildSHA256     string
}

// Sum is the last CSV line.
type Sum struct {
	Belegbetrag int
	Anerkannt   int
	Erstattung  int
	Eigenanteil int
	GV          int
	Steuerfrei  int
	Regulaer    int
}

// CSV returns UTF-8 with BOM, semicolon separators, decimal commas, and CRLF.
func CSV(rows []Row, sum Sum) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	buf.WriteString(CSVHeader)
	buf.WriteString("\r\n")
	for _, row := range rows {
		writeLine(&buf, []string{
			strconv.Itoa(row.Nr),
			row.Datum,
			row.Wochentag,
			row.Mahlzeit,
			row.Bezugsort,
			row.Arbeitsort,
			row.Haendler,
			row.Ort,
			Decimal(row.Belegbetrag),
			Decimal(row.Anerkannt),
			row.KorrekturGrund,
			Decimal(row.Erstattung),
			Decimal(row.Eigenanteil),
			Decimal(row.GV),
			Decimal(row.Steuerfrei),
			Decimal(row.Regulaer),
			row.Warnungen,
			row.BildSHA256,
		})
	}
	writeLine(&buf, []string{
		"summe", "", "", "", "", "", "", "",
		Decimal(sum.Belegbetrag),
		Decimal(sum.Anerkannt),
		"",
		Decimal(sum.Erstattung),
		Decimal(sum.Eigenanteil),
		Decimal(sum.GV),
		Decimal(sum.Steuerfrei),
		Decimal(sum.Regulaer),
		"", "",
	})
	return buf.Bytes()
}

// Decimal formats cents with a comma and no thousands separator.
func Decimal(cents int) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d,%02d", sign, cents/100, cents%100)
}

func writeLine(buf *bytes.Buffer, fields []string) {
	for i, field := range fields {
		if i > 0 {
			buf.WriteByte(';')
		}
		buf.WriteString(escape(field))
	}
	buf.WriteString("\r\n")
}

func escape(field string) string {
	if strings.ContainsAny(field, ";\"\r\n") {
		return `"` + strings.ReplaceAll(field, `"`, `""`) + `"`
	}
	return field
}
