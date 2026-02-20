package main

import (
	"strings"
	"testing"
)

func TestTailBufferKeepsLastBytes(t *testing.T) {
	tb := newTailBuffer(5)
	if _, err := tb.Write([]byte("abc")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if _, err := tb.Write([]byte("defg")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if got := tb.String(); got != "cdefg" {
		t.Fatalf("tail = %q, want %q", got, "cdefg")
	}
}

func TestTailBufferHandlesLargeChunk(t *testing.T) {
	tb := newTailBuffer(4)
	if _, err := tb.Write([]byte("123456")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if got := tb.String(); got != "3456" {
		t.Fatalf("tail = %q, want %q", got, "3456")
	}
}

func TestTailBufferZeroLimit(t *testing.T) {
	tb := newTailBuffer(0)
	if _, err := tb.Write([]byte(strings.Repeat("x", 10))); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if got := tb.String(); got != "" {
		t.Fatalf("tail = %q, want empty", got)
	}
}
