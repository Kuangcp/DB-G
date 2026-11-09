package app_test

import (
	"context"
	"encoding/json"
	"testing"

	"db-g/app"
)

func TestInsertStatementJSONRoundTripSQLite(t *testing.T) {
	s := openEditSession(t)
	if _, err := s.RunStatement(context.Background(), `CREATE TABLE meeting_case3 (id INTEGER, payload TEXT)`); err != nil {
		t.Fatal(err)
	}
	payload := `{"path":"C:\\tmp","quote":"say \"hi\"","name":"O'Reilly"}`
	stmt := app.InsertStatement("sqlite", "meeting_case3", []string{"id", "payload"}, []any{int64(1), payload})
	if _, err := s.RunStatement(context.Background(), stmt); err != nil {
		t.Fatalf("insert failed: %v\n%s", err, stmt)
	}
	res, err := s.RunStatement(context.Background(), `SELECT payload FROM meeting_case3 WHERE id = 1`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || len(res.Rows[0]) != 1 {
		t.Fatalf("rows: %+v", res.Rows)
	}
	var got string
	switch v := res.Rows[0][0].(type) {
	case string:
		got = v
	case []byte:
		got = string(v)
	default:
		t.Fatalf("payload type: %T", v)
	}
	if got != payload {
		t.Fatalf("payload changed:\n got %s\nwant %s", got, payload)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("payload is not valid JSON: %s", got)
	}
}
