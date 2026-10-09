package service

import "time"

// DefaultExportRetentionYears is the app recommendation in
// docs/research/steuer.md §2.6: keep Monatsexport data at least ten years.
// The Lohnkonto minimum in § 41 Abs. 1 EStG is six calendar years; set
// BELEGAPP_EXPORT_RETENTION_YEARS=6 for that clock. The app does not delete
// when the window ends (SPEC §5.2).
const DefaultExportRetentionYears = 10

// RetentionDeadline is the end of calendar year year(at)+years in loc.
// A 2026 export with years=6 lasts through 2032-12-31 in that zone, matching
// "Ablauf des 6. Kalenderjahres" in the research notes. elapsed is true once
// now is after that instant. years below 1 uses DefaultExportRetentionYears.
func RetentionDeadline(now, at time.Time, years int, loc *time.Location) (until time.Time, elapsed bool) {
	if loc == nil {
		loc = time.UTC
	}
	if years < 1 {
		years = DefaultExportRetentionYears
	}
	local := at.In(loc)
	until = time.Date(local.Year()+years, time.December, 31, 23, 59, 59, 0, loc)
	return until, now.After(until)
}

func (s *Service) retentionYears() int {
	if s == nil || s.RetentionYears < 1 {
		return DefaultExportRetentionYears
	}
	return s.RetentionYears
}

func (s *Service) retentionOf(erstellt string) (until string, elapsed bool) {
	at, err := time.Parse(time.RFC3339, erstellt)
	if err != nil {
		return "", false
	}
	deadline, done := RetentionDeadline(s.now(), at, s.retentionYears(), s.Loc)
	return deadline.UTC().Format(time.RFC3339), done
}
