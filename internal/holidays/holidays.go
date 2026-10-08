package holidays

import (
	"slices"
	"sync"
	"time"

	"github.com/rickar/cal/v2"
	"github.com/rickar/cal/v2/de"
)

// Feiertag is a holiday date in the business timezone's calendar.
type Feiertag struct {
	Datum string `json:"datum"`
	Name  string `json:"name"`
}

// Provider returns statutory holidays for a year and Bundesland.
type Provider interface {
	Feiertage(jahr int, land string) []Feiertag
}

// Calendar is the rickar/cal wrapper.
type Calendar struct {
	mu    sync.Mutex
	cache map[string][]Feiertag
}

// NewCalendar returns an empty cache.
func NewCalendar() *Calendar {
	return &Calendar{cache: map[string][]Feiertag{}}
}

var states = map[string][]*cal.Holiday{
	"BW": de.HolidaysBW,
	"BY": de.HolidaysBY,
	"BE": de.HolidaysBE,
	"BB": de.HolidaysBB,
	"HB": de.HolidaysHB,
	"HH": de.HolidaysHH,
	"HE": de.HolidaysHE,
	"MV": de.HolidaysMV,
	"NI": de.HolidaysNI,
	"NW": de.HolidaysNW,
	"RP": de.HolidaysRP,
	"SL": de.HolidaysSL,
	"SN": de.HolidaysSN,
	"ST": de.HolidaysST,
	"SH": de.HolidaysSH,
	"TH": de.HolidaysTH,
}

// Feiertage returns statutory holidays. Unknown states return nil.
// Frauentag in Mecklenburg-Vorpommern is added because rickar/cal v2.1.32 omits
// that statewide holiday (in force since 2023).
func (c *Calendar) Feiertage(jahr int, land string) []Feiertag {
	base, ok := states[land]
	if !ok {
		return nil
	}
	key := land + ":" + itoa(jahr)
	c.mu.Lock()
	defer c.mu.Unlock()
	if hit, ok := c.cache[key]; ok {
		return clone(hit)
	}
	list := append([]*cal.Holiday{}, base...)
	if land == "MV" && jahr >= 2023 {
		list = append(list, de.Frauentag)
	}
	out := make([]Feiertag, 0, len(list))
	for _, holiday := range list {
		actual, _ := holiday.Calc(jahr)
		if actual.IsZero() {
			continue
		}
		y, m, d := actual.Date()
		if y != jahr {
			continue
		}
		out = append(out, Feiertag{
			Datum: time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
			Name:  holiday.Name,
		})
	}
	slices.SortFunc(out, func(a, b Feiertag) int {
		if a.Datum < b.Datum {
			return -1
		}
		if a.Datum > b.Datum {
			return 1
		}
		return 0
	})
	c.cache[key] = out
	return clone(out)
}

// Merge adds custom holidays. A custom date that is already statutory keeps the statutory name.
func Merge(base []Feiertag, extra []Feiertag, jahr int) []Feiertag {
	out := clone(base)
	seen := map[string]bool{}
	for _, tag := range out {
		seen[tag.Datum] = true
	}
	for _, tag := range extra {
		if len(tag.Datum) < 4 || tag.Datum[:4] != itoa(jahr) || seen[tag.Datum] {
			continue
		}
		seen[tag.Datum] = true
		out = append(out, tag)
	}
	slices.SortFunc(out, func(a, b Feiertag) int {
		if a.Datum < b.Datum {
			return -1
		}
		if a.Datum > b.Datum {
			return 1
		}
		return 0
	})
	return out
}

// Lookup returns the holiday name for a date.
func Lookup(days []Feiertag, datum string) (string, bool) {
	for _, tag := range days {
		if tag.Datum == datum {
			return tag.Name, true
		}
	}
	return "", false
}

func clone(in []Feiertag) []Feiertag {
	if in == nil {
		return nil
	}
	return slices.Clone(in)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	for i := 3; i >= 0; i-- {
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[:])
}
