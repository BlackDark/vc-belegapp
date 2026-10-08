package holidays

import "testing"

func TestAllStates2026(t *testing.T) {
	cal := NewCalendar()
	states := []string{"BW", "BY", "BE", "BB", "HB", "HH", "HE", "MV", "NI", "NW", "RP", "SL", "SN", "ST", "SH", "TH"}
	national := []string{"2026-01-01", "2026-04-03", "2026-04-06", "2026-05-01", "2026-05-14", "2026-05-25", "2026-10-03", "2026-12-25", "2026-12-26"}
	for _, land := range states {
		days := cal.Feiertage(2026, land)
		if len(days) < len(national) {
			t.Fatalf("%s only %d", land, len(days))
		}
		set := map[string]string{}
		for _, day := range days {
			set[day.Datum] = day.Name
		}
		for _, datum := range national {
			if set[datum] == "" {
				t.Fatalf("%s missing %s", land, datum)
			}
		}
		if set["2026-10-03"] != "Tag der Deutschen Einheit" {
			t.Fatalf("%s unity %q", land, set["2026-10-03"])
		}
	}
	nw := index(cal.Feiertage(2026, "NW"))
	if nw["2026-06-04"] != "Fronleichnam" || nw["2026-11-01"] != "Allerheiligen" || nw["2026-10-31"] != "" {
		t.Fatalf("nw %+v", nw)
	}
	if index(cal.Feiertage(2026, "BY"))["2026-08-15"] != "" {
		t.Fatal("BY should not include partial Mariä Himmelfahrt")
	}
	if index(cal.Feiertage(2026, "SL"))["2026-08-15"] != "Mariä Himmelfahrt" {
		t.Fatal("SL assumption")
	}
	if index(cal.Feiertage(2026, "SN"))["2026-11-18"] != "Buß- und Bettag" {
		t.Fatal("SN repentance day")
	}
	if index(cal.Feiertage(2026, "BE"))["2026-03-08"] != "Frauentag" {
		t.Fatal("BE women's day")
	}
	if index(cal.Feiertage(2026, "MV"))["2026-03-08"] != "Frauentag" {
		t.Fatal("MV women's day")
	}
	if index(cal.Feiertage(2026, "BW"))["2026-01-06"] != "Heilige Drei Könige" {
		t.Fatal("BW epiphany")
	}
	if index(cal.Feiertage(2026, "HH"))["2026-01-06"] != "" {
		t.Fatal("HH epiphany")
	}
	if index(cal.Feiertage(2026, "TH"))["2026-09-20"] != "Weltkindertag" {
		t.Fatal("TH children's day")
	}
	if cal.Feiertage(2026, "XX") != nil {
		t.Fatal("unknown state")
	}
	again := cal.Feiertage(2026, "NW")
	again[0].Name = "changed"
	if cal.Feiertage(2026, "NW")[0].Name == "changed" {
		t.Fatal("cache aliased")
	}
}

func TestMergeCustom(t *testing.T) {
	cal := NewCalendar()
	base := cal.Feiertage(2026, "NW")
	merged := Merge(base, []Feiertag{
		{Datum: "2026-08-15", Name: "Mariä Himmelfahrt (lokal)"},
		{Datum: "2026-10-03", Name: "soll nicht gewinnen"},
		{Datum: "2025-01-01", Name: "falsches Jahr"},
	}, 2026)
	got := index(merged)
	if got["2026-08-15"] != "Mariä Himmelfahrt (lokal)" || got["2026-10-03"] != "Tag der Deutschen Einheit" {
		t.Fatalf("%+v", got)
	}
	if _, ok := got["2025-01-01"]; ok {
		t.Fatal("wrong year kept")
	}
	name, ok := Lookup(merged, "2026-08-15")
	if !ok || name == "" {
		t.Fatal(name)
	}
}

func index(days []Feiertag) map[string]string {
	out := map[string]string{}
	for _, day := range days {
		out[day.Datum] = day.Name
	}
	return out
}
