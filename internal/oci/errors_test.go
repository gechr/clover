package oci_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/oci"
	"github.com/stretchr/testify/require"
)

func TestStatusErr(t *testing.T) {
	t.Parallel()

	client := oci.New(oci.WithErrorContext("oci", "authenticate for higher limits"))

	tests := map[string]struct {
		status string
		code   int
		want   string
	}{
		"unauthorized appends the hint": {
			code:   http.StatusUnauthorized,
			status: "401 Unauthorized",
			want:   "oci: list tags: 401 Unauthorized (authenticate for higher limits)",
		},
		"forbidden appends the hint": {
			code:   http.StatusForbidden,
			status: "403 Forbidden",
			want:   "oci: list tags: 403 Forbidden (authenticate for higher limits)",
		},
		"too many requests appends the hint": {
			code:   http.StatusTooManyRequests,
			status: "429 Too Many Requests",
			want:   "oci: list tags: 429 Too Many Requests (authenticate for higher limits)",
		},
		"server error omits the hint": {
			code:   http.StatusInternalServerError,
			status: "500 Internal Server Error",
			want:   "oci: list tags: 500 Internal Server Error",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := client.StatusErr(
				"list tags",
				&http.Response{StatusCode: tt.code, Status: tt.status},
			)
			require.EqualError(t, err, tt.want)
		})
	}
}
