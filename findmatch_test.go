package main

import (
	"testing"
)

func TestFindAll(t *testing.T) {
	s := "foo xx foo"
	ranges := FindAll(s, "foo")
	if len(ranges) != 2 {
		t.Fatalf("got %d want 2", len(ranges))
	}
	if ranges[0].Start != 0 || ranges[0].End != 3 {
		t.Errorf("ranges[0]=%+v", ranges[0])
	}
	if ranges[1].Start != 7 || ranges[1].End != 10 {
		t.Errorf("ranges[1]=%+v", ranges[1])
	}
	if FindAll(s, "") != nil {
		t.Fatal("empty query must return nil")
	}
	if FindAll(s, "missing") != nil {
		t.Fatal("no match must return nil")
	}
}

func TestFindAllNonASCII(t *testing.T) {
	s := "用户 用户"
	ranges := FindAll(s, "用户")
	if len(ranges) != 2 {
		t.Fatalf("got %d want 2", len(ranges))
	}
	if ranges[0].Start != 0 || ranges[0].End != 2 {
		t.Errorf("ranges[0]=%+v", ranges[0])
	}
	if ranges[1].Start != 3 || ranges[1].End != 5 {
		t.Errorf("ranges[1]=%+v", ranges[1])
	}
}