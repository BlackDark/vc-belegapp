package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/erkennung"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/imaging"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/rules"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

func openService(t *testing.T, extractor erkennung.ReceiptExtractor) *Service {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "belegapp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, database.Write); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureSystem(ctx, database.Write, nil); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 10, 8, 15, 0, 0, 0, loc)
	return &Service{
		DB:         database,
		Store:      storage.NewFS(t.TempDir()),
		Holidays:   holidays.NewCalendar(),
		Loc:        loc,
		Now:        func() time.Time { return fixed },
		UploadMax:  1 << 20,
		ImageTTL:   time.Hour,
		Extractor:  extractor,
		LLMMaxPX:   1600,
		LLMTimeout: time.Second,
	}
}

func startJobs(t *testing.T, svc *Service) {
	t.Helper()
	svc.Jobs = &jobs.Queue{
		DB:      svc.DB,
		Workers: 1,
		Poll:    10 * time.Millisecond,
		Backoff: func(int, string) time.Duration { return 5 * time.Millisecond },
		Handle:  svc.HandleJob,
		OnRetry: svc.JobRetry,
		OnFail:  svc.JobFail,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.RunJobs(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func waitStatus(t *testing.T, svc *Service, id, status string) Bild {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	var last Bild
	for time.Now().Before(deadline) {
		bild, err := svc.GetBild(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		last = bild
		if bild.Erkennung.Status == status {
			return bild
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("status %s, want %s (%v)", last.Erkennung.Status, status, last.Erkennung.Fehler)
	return last
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	img.Set(1, 1, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type scriptedLLM struct {
	mu    sync.Mutex
	steps []http.HandlerFunc
	seen  [][]byte
}

func (s *scriptedLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.seen = append(s.seen, body)
	n := len(s.seen) - 1
	s.mu.Unlock()
	if n >= len(s.steps) {
		http.Error(w, "extra", http.StatusInternalServerError)
		return
	}
	s.steps[n](w, r)
}

func (s *scriptedLLM) body(i int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[i]
}

func (s *scriptedLLM) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func llmOK(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(erkennung.CompletionBody(content))
	}
}

func llmStatus(status int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": message, "type": "api_error"},
		})
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(erkennung.ExampleErgebnis("2026-10-07"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func clientFor(t *testing.T, srv *httptest.Server) *erkennung.OpenAI {
	t.Helper()
	return erkennung.NewOpenAI(erkennung.OpenAIOptions{
		BaseURL:    srv.URL + "/v1",
		APIKey:     "test",
		Model:      "gpt-5-mini",
		Format:     "json_schema",
		Timeout:    time.Second,
		HTTPClient: srv.Client(),
	})
}

func TestRecognition429ThenSuccess(t *testing.T) {
	content := fixture(t)
	fake := &scriptedLLM{steps: []http.HandlerFunc{
		llmStatus(http.StatusTooManyRequests, "slow down"),
		llmOK(content),
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	svc := openService(t, clientFor(t, srv))
	startJobs(t, svc)
	bild, err := svc.SaveBild(context.Background(), tinyPNG(t), true)
	if err != nil {
		t.Fatal(err)
	}
	if bild.Erkennung.Status != "ausstehend" {
		t.Fatalf("upload %s", bild.Erkennung.Status)
	}
	done := waitStatus(t, svc, bild.ID, "fertig")
	if done.Erkennung.Ergebnis == nil || done.Erkennung.Ergebnis.HaendlerName == nil || *done.Erkennung.Ergebnis.HaendlerName != "Edeka" {
		t.Fatalf("%+v", done.Erkennung.Ergebnis)
	}
	if done.Erkennung.KorrekturvorschlagCent == nil || *done.Erkennung.KorrekturvorschlagCent != 1275 {
		t.Fatalf("suggestion %v", done.Erkennung.KorrekturvorschlagCent)
	}
	row, err := db.New(svc.DB.Write).GetBelegbild(context.Background(), bild.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asString(row.ErkennungRoh), "Edeka") {
		t.Fatal("raw missing")
	}
	encoded, _ := json.Marshal(done)
	if strings.Contains(string(encoded), "erkennung_roh") {
		t.Fatal("raw leaked")
	}
	if fake.calls() != 2 {
		t.Fatalf("calls %d", fake.calls())
	}
	sent := sentJPEG(t, fake.body(1))
	if bytes.Contains(sent, []byte("Exif")) || bytes.HasPrefix(sent, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("original or exif was sent")
	}
	rc, _, err := svc.Store.Get(context.Background(), row.BlobKey)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := io.ReadAll(rc)
	_ = rc.Close()
	want, err := imaging.ForLLM(stored, 1600)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sent, want) {
		t.Fatal("model image is not the re-encoded jpeg")
	}

	regel := rules.Vorschlag(2026, nil)
	if _, err := svc.PutJahresregel(context.Background(), Actor{Name: "test"}, regel); err != nil {
		t.Fatal(err)
	}
	name := "Edeka"
	ort := "Köln"
	in := BelegInput{
		Datum: "2026-10-07", Mahlzeit: "mittag", Bezugsort: "supermarkt", Arbeitsort: "betrieb",
		HaendlerName: name, HaendlerOrt: ort, BelegbetragCent: 1490, BildIDs: []string{bild.ID},
	}
	beleg, err := svc.CreateBeleg(context.Background(), Actor{Name: "test"}, in)
	if err != nil {
		t.Fatal(err)
	}
	if beleg.Quelle != "ki" {
		t.Fatalf("quelle %s", beleg.Quelle)
	}
	changed := 1000
	patched, err := svc.UpdateBeleg(context.Background(), Actor{Name: "test"}, beleg.ID, BelegPatch{
		Version: beleg.Version, BelegbetragCent: &changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if patched.Quelle != "ki_korrigiert" {
		t.Fatalf("quelle %s", patched.Quelle)
	}
}

func TestRecognitionMalformedThenSuccess(t *testing.T) {
	fake := &scriptedLLM{steps: []http.HandlerFunc{llmOK("kein json"), llmOK(fixture(t))}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	svc := openService(t, clientFor(t, srv))
	startJobs(t, svc)
	bild, err := svc.SaveBild(context.Background(), tinyPNG(t), true)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, svc, bild.ID, "fertig")
	if fake.calls() != 2 {
		t.Fatalf("calls %d", fake.calls())
	}
}

func TestRecognitionAuthNoRetry(t *testing.T) {
	fake := &scriptedLLM{steps: []http.HandlerFunc{llmStatus(http.StatusUnauthorized, "bad key")}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := clientFor(t, srv)
	svc := openService(t, client)
	startJobs(t, svc)
	bild, err := svc.SaveBild(context.Background(), tinyPNG(t), true)
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, svc, bild.ID, "fehler")
	if done.Erkennung.Fehler == nil || *done.Erkennung.Fehler != "API-Key ungültig" {
		t.Fatalf("%v", done.Erkennung.Fehler)
	}
	if fake.calls() != 1 {
		t.Fatalf("calls %d", fake.calls())
	}
	if svc.ErkennungProblem() != "API-Key ungültig" {
		t.Fatal(svc.ErkennungProblem())
	}
}

func TestRecognitionStaysManual(t *testing.T) {
	fake := &scriptedLLM{steps: []http.HandlerFunc{llmOK(fixture(t))}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	svc := openService(t, erkennung.Disabled{})
	startJobs(t, svc)
	bild, err := svc.SaveBild(context.Background(), tinyPNG(t), true)
	if err != nil {
		t.Fatal(err)
	}
	if bild.Erkennung.Status != "keine" {
		t.Fatalf("%s", bild.Erkennung.Status)
	}
	if fake.calls() != 0 {
		t.Fatal("disabled extractor called the model")
	}
	test, err := svc.TestErkennung(context.Background())
	if err != nil || test.OK || test.Fehler == nil {
		t.Fatalf("%+v %v", test, err)
	}

	active := openService(t, clientFor(t, srv))
	off, err := active.SaveBild(context.Background(), tinyPNG(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if off.Erkennung.Status != "keine" {
		t.Fatalf("flag %s", off.Erkennung.Status)
	}
	if _, err := active.DB.Write.Exec(`UPDATE einstellungen SET erkennung_aktiv = 0`); err != nil {
		t.Fatal(err)
	}
	paused, err := active.SaveBild(context.Background(), tinyPNG(t), true)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Erkennung.Status != "keine" {
		t.Fatalf("setting %s", paused.Erkennung.Status)
	}
	if fake.calls() != 0 {
		t.Fatalf("calls %d", fake.calls())
	}
}

func sentJPEG(t *testing.T, body []byte) []byte {
	t.Helper()
	const marker = "data:image/jpeg;base64,"
	text := string(body)
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatal("data url missing")
	}
	rest := text[i+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatal("data url end")
	}
	raw, err := base64.StdEncoding.DecodeString(rest[:end])
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
