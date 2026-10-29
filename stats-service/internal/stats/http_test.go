package stats

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

var linkA = uuid.MustParse("7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11")
var linkB = uuid.MustParse("0d9a6c1e-8f3b-4f0a-a1c2-5e6d7f8091ab")

type fakeRepo struct {
	mu            sync.Mutex
	clicks        map[uuid.UUID]Click
	fail          bool
	failWrites    int
	writes, reads int
	delay         time.Duration
}

func (f *fakeRepo) InsertBatch(_ context.Context, clicks []Click) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.fail || f.failWrites > 0 {
		f.failWrites--
		return errors.New("db down")
	}
	if f.clicks == nil {
		f.clicks = map[uuid.UUID]Click{}
	}
	for _, c := range clicks {
		f.clicks[c.EventID] = c
	}
	return nil
}
func (f *fakeRepo) Totals(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("db down")
	}
	result := map[uuid.UUID]int64{}
	for _, c := range f.clicks {
		for _, id := range ids {
			if c.LinkID == id {
				result[id]++
			}
		}
	}
	return result, nil
}
func (f *fakeRepo) Summary(_ context.Context, id uuid.UUID) (Summary, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	result := Summary{LinkID: id.String(), ClicksPerDay: []DayCount{}}
	if f.fail {
		return result, errors.New("db down")
	}
	days := map[string]int64{}
	for _, c := range f.clicks {
		if c.LinkID == id {
			days[c.ClickedAt.UTC().Format("2006-01-02")]++
			result.TotalClicks++
		}
	}
	for day, n := range days {
		result.ClicksPerDay = append(result.ClicksPerDay, DayCount{day, n})
	}
	return result, nil
}

type fakeCache struct {
	mu      sync.Mutex
	values  map[uuid.UUID][]byte
	fail    bool
	deleted []uuid.UUID
}

func (f *fakeCache) Get(_ context.Context, id uuid.UUID) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, "", errors.New("redis down")
	}
	return append([]byte(nil), f.values[id]...), "0", nil
}
func (f *fakeCache) Set(_ context.Context, id uuid.UUID, _ string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("redis down")
	}
	if f.values == nil {
		f.values = map[uuid.UUID][]byte{}
	}
	f.values[id] = append([]byte(nil), value...)
	return nil
}
func (f *fakeCache) Invalidate(_ context.Context, ids []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("redis down")
	}
	for _, id := range ids {
		delete(f.values, id)
		f.deleted = append(f.deleted, id)
	}
	return nil
}

func TestHTTPContract(t *testing.T) {
	r := &fakeRepo{}
	_ = r.InsertBatch(context.Background(), []Click{
		{uuid.New(), linkA, "aB3xK9q", time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)},
		{uuid.New(), linkA, "aB3xK9q", time.Date(2026, 10, 7, 22, 30, 0, 0, time.FixedZone("BR", -3*3600))},
		{uuid.New(), linkB, "bB3xK9q", time.Now()},
	})
	h := newHandler(r, &fakeCache{})
	for _, path := range []string{"/stats", "/stats?linkIds=", "/stats?linkIds=,", "/stats?linkIds=" + linkA.String() + ",abc", "/stats/abc"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 400 {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			var body struct{ Error struct{ Code string } }
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Fatal(w.Body)
			}
		})
	}
	t.Run("totals order duplicates and zeros", func(t *testing.T) {
		unknown := uuid.New()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/stats?linkIds="+linkB.String()+","+unknown.String()+","+linkA.String()+","+linkA.String(), nil))
		var totals []Total
		if err := json.Unmarshal(w.Body.Bytes(), &totals); err != nil {
			t.Fatal(err)
		}
		if len(totals) != 3 || totals[0].TotalClicks != 1 || totals[1].TotalClicks != 0 || totals[2].TotalClicks != 2 {
			t.Fatal(totals)
		}
	})
	t.Run("summary cache", func(t *testing.T) {
		for i, expected := range []string{"MISS", "HIT"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/stats/"+linkA.String(), nil))
			var body Summary
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != 200 || body.TotalClicks != 2 || len(body.ClicksPerDay) != 2 || w.Header().Get("X-Cache") != expected {
				t.Fatalf("request %d: %s %v", i, w.Body, w.Header())
			}
		}
	})
	t.Run("unknown summary empty array", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/stats/"+uuid.NewString(), nil))
		var body Summary
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 200 || body.TotalClicks != 0 || body.ClicksPerDay == nil || len(body.ClicksPerDay) != 0 {
			t.Fatal(w.Body)
		}
	})
	t.Run("health", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
		if w.Code != 200 || w.Body.String() != "{\"service\":\"stats-service\",\"status\":\"ok\"}\n" {
			t.Fatal(w.Body)
		}
	})
}

func TestCacheFailureFallsBack(t *testing.T) {
	h := newHandler(&fakeRepo{}, &fakeCache{fail: true})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/stats/"+linkA.String(), nil))
	if w.Code != 200 || w.Header().Get("X-Cache") != "MISS" {
		t.Fatal(w.Code, w.Body)
	}
}
func TestDatabaseFailure(t *testing.T) {
	h := newHandler(&fakeRepo{fail: true}, &fakeCache{})
	for _, path := range []string{"/stats/" + linkA.String(), "/stats?linkIds=" + linkA.String()} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 500 {
			t.Fatal(w.Code, w.Body)
		}
	}
}
func TestConcurrentCacheMissIsCoalesced(t *testing.T) {
	r := &fakeRepo{delay: 50 * time.Millisecond}
	h := newHandler(r, &fakeCache{})
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/stats/"+linkA.String(), nil))
			if w.Code != 200 {
				t.Errorf("status %d", w.Code)
			}
		})
	}
	wg.Wait()
	if r.reads != 1 {
		t.Fatalf("queries=%d, want 1", r.reads)
	}
}

func BenchmarkParseClick(b *testing.B) {
	data := []byte(`{"eventId":"11111111-1111-4111-8111-111111111111","linkId":"7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11","slug":"aB3xK9q","clickedAt":"2026-10-28T12:00:00Z"}`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseClick(data); err != nil {
			b.Fatal(err)
		}
	}
}
