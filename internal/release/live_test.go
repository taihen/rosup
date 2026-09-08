//go:build live

package release_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/release"
)

func TestLiveNewestLongTerm(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := release.NewHTTPClient()
	body, status, err := c.Get(ctx, release.NewestURL())
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	ver, err := release.ParseNewest(body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("newest long-term %s", ver)
}
