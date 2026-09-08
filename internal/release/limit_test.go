package release

import (
	"strings"
	"testing"
)

func TestReadLimitedFailsWhenCapHit(t *testing.T) {
	_, err := readLimited(strings.NewReader(strings.Repeat("x", 11)), 10)
	if err == nil {
		t.Fatal("expected error when body exceeds cap")
	}
}

func TestReadLimitedOKUnderCap(t *testing.T) {
	got, err := readLimited(strings.NewReader("abc"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestBodyLimitNPKVsMeta(t *testing.T) {
	if bodyLimit("https://example/a.npk") != maxNPKBody {
		t.Fatal("npk should use large cap")
	}
	if bodyLimit("https://example/NEWEST6.long-term") != maxMetaBody {
		t.Fatal("meta should use small cap")
	}
	if bodyLimit("https://download.mikrotik.com/routeros/6.49.21/") != maxMetaBody {
		t.Fatal("listing should use small cap")
	}
}
