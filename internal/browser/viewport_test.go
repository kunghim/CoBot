package browser

import (
	"errors"
	"testing"
)

func TestResolveViewport(t *testing.T) {
	cases := []struct {
		name    string
		req     ViewportRequest
		want    *Viewport
		wantErr bool
	}{
		{name: "desktop preset", req: ViewportRequest{Preset: "desktop"}, want: &Viewport{Width: 1280, Height: 744}},
		{name: "tablet preset", req: ViewportRequest{Preset: "tablet"}, want: &Viewport{Width: 768, Height: 1024}},
		{name: "phone preset", req: ViewportRequest{Preset: "phone"}, want: &Viewport{Width: 390, Height: 844, Mobile: true}},
		{name: "clear returns the default window", req: ViewportRequest{Preset: "clear"}, want: nil},
		{name: "explicit size", req: ViewportRequest{Width: 800, Height: 600}, want: &Viewport{Width: 800, Height: 600}},
		{name: "explicit mobile size", req: ViewportRequest{Width: 414, Height: 896, Mobile: true}, want: &Viewport{Width: 414, Height: 896, Mobile: true}},
		{name: "unknown preset", req: ViewportRequest{Preset: "watch"}, wantErr: true},
		{name: "nothing given", req: ViewportRequest{}, wantErr: true},
		{name: "half a size", req: ViewportRequest{Width: 800}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveViewport(tc.req)
			if tc.wantErr {
				if !errors.Is(err, ErrBadRequest) {
					t.Fatalf("error = %v, want ErrBadRequest", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("viewport = %+v, want %+v", got, tc.want)
			}
			if got != nil && *got != *tc.want {
				t.Fatalf("viewport = %+v, want %+v", *got, *tc.want)
			}
		})
	}
}
