package stats

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func validMessage() kafka.Message {
	return kafka.Message{Topic: "link.clicked", Offset: 5, Value: []byte(`{"eventId":"11111111-1111-4111-8111-111111111111","linkId":"7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11","slug":"aB3xK9q","clickedAt":"2026-10-07T22:30:00-03:00"}`)}
}

func TestEventValidation(t *testing.T) {
	good := validMessage()
	c, err := parseClick(good.Value)
	if err != nil || c.ClickedAt.Format(time.RFC3339) != "2026-10-08T01:30:00Z" {
		t.Fatal(c, err)
	}
	for _, field := range []string{"eventId", "linkId", "slug", "clickedAt"} {
		t.Run(field, func(t *testing.T) {
			var obj map[string]any
			_ = json.Unmarshal(good.Value, &obj)
			obj[field] = "bad!"
			data, _ := json.Marshal(obj)
			if _, err := parseClick(data); err == nil {
				t.Fatal("accepted invalid ", field)
			}
			delete(obj, field)
			data, _ = json.Marshal(obj)
			if _, err := parseClick(data); err == nil {
				t.Fatal("accepted missing ", field)
			}
		})
	}
	for _, data := range [][]byte{nil, []byte("not json"), []byte("null"), []byte(`{"clickedAt":"2026-10-28T12:00:00"}`)} {
		if _, err := parseClick(data); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestProcessDuplicateAndInvalid(t *testing.T) {
	r, c := &fakeRepo{}, &fakeCache{}
	m := validMessage()
	if err := processBatch(context.Background(), r, c, []kafka.Message{m, m, {Value: []byte("bad")}}); err != nil {
		t.Fatal(err)
	}
	if len(r.clicks) != 1 || len(c.deleted) != 1 {
		t.Fatal(len(r.clicks), c.deleted)
	}
}
func TestProcessDatabaseFailureDoesNotInvalidate(t *testing.T) {
	r, c := &fakeRepo{fail: true}, &fakeCache{}
	if err := processBatch(context.Background(), r, c, []kafka.Message{validMessage()}); err == nil {
		t.Fatal("expected failure")
	}
	if len(c.deleted) != 0 {
		t.Fatal(c.deleted)
	}
}
func TestProcessCacheFailureIsNotFatal(t *testing.T) {
	if err := processBatch(context.Background(), &fakeRepo{}, &fakeCache{fail: true}, []kafka.Message{validMessage()}); err != nil {
		t.Fatal(err)
	}
}

type fakeReader struct {
	message    kafka.Message
	fetched    bool
	repo       *fakeRepo
	commits    int
	commitFail bool
	cancel     context.CancelFunc
}

func (f *fakeReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if !f.fetched {
		f.fetched = true
		return f.message, nil
	}
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}
func (f *fakeReader) CommitMessages(_ context.Context, messages ...kafka.Message) error {
	if len(f.repo.clicks) != 1 {
		panic("commit before persistence")
	}
	f.commits++
	if f.commitFail && f.commits == 1 {
		return errors.New("temporary commit failure")
	}
	f.cancel()
	return nil
}
func TestConsumerRetriesBeforeCommitting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := &fakeRepo{failWrites: 1}
	reader := &fakeReader{message: validMessage(), repo: r, commitFail: true, cancel: cancel}
	consume(ctx, reader, r, &fakeCache{})
	if r.writes != 2 || reader.commits != 2 {
		t.Fatal(r.writes, reader.commits)
	}
}
func TestTracePropagation(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	m := validMessage()
	m.Headers = []kafka.Header{{Key: "TraceParent", Value: []byte("00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")}}
	sc := trace.SpanContextFromContext(eventContext(context.Background(), m))
	if sc.TraceID().String() != "0123456789abcdef0123456789abcdef" {
		t.Fatal(sc)
	}
	m.Headers = nil
	if trace.SpanContextFromContext(eventContext(context.Background(), m)).IsValid() {
		t.Fatal("unexpected parent")
	}
}
