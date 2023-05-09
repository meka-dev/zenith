package api

import (
	"context"
	"strconv"
	"testing"
)

func TestGetBestMediaType(t *testing.T) {
	for i, testcase := range []struct {
		inputValues       []string
		prioritizedValues []string
		wantValue         string
	}{
		{nil, nil, ""},
		{nil, []string{"text/plain"}, ""},
		{[]string{}, []string{"text/plain"}, ""},
		{[]string{"application/json"}, []string{}, "application/json"},
		{[]string{"application/json"}, []string{"text/plain"}, "application/json"},
		{[]string{"application/json; charset=utf-8"}, []string{"text/plain"}, "application/json"},
		{[]string{"application/json; charset=utf-8"}, []string{"application/json"}, "application/json"},
		{[]string{"text/plain", "application/json; charset=utf-8"}, []string{"application/json"}, "application/json"},
		{[]string{"text/plain", "application/json; charset=utf-8"}, []string{"application/json; charset=utf-16"}, "application/json"},
	} {
		t.Run(strconv.Itoa(i+1), func(t *testing.T) {
			ctx := context.Background()
			want := testcase.wantValue
			have := getBestMediaType(ctx, testcase.inputValues, testcase.prioritizedValues...)
			if want != have {
				t.Errorf("%v, %v: want %q, have %q", testcase.inputValues, testcase.prioritizedValues, want, have)
			}
		})
	}
}
