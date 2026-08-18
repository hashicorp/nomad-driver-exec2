// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package capabilities

import (
	"testing"

	"github.com/shoenig/test/must"
	"golang.org/x/sys/unix"
)

func TestParseCap(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expVal uintptr
		expErr string
	}{
		{
			name:   "known cap lowercase",
			input:  "net_bind_service",
			expVal: unix.CAP_NET_BIND_SERVICE,
		},
		{
			name:   "known cap uppercase",
			input:  "NET_BIND_SERVICE",
			expVal: unix.CAP_NET_BIND_SERVICE,
		},
		{
			name:   "known cap mixed case",
			input:  "Net_Bind_Service",
			expVal: unix.CAP_NET_BIND_SERVICE,
		},
		{
			name:   "chown",
			input:  "chown",
			expVal: unix.CAP_CHOWN,
		},
		{
			name:   "sys_admin",
			input:  "sys_admin",
			expVal: unix.CAP_SYS_ADMIN,
		},
		{
			name:   "checkpoint_restore",
			input:  "checkpoint_restore",
			expVal: unix.CAP_CHECKPOINT_RESTORE,
		},
		{
			name:   "cap_ prefix lowercase accepted",
			input:  "cap_chown",
			expVal: unix.CAP_CHOWN,
		},
		{
			name:   "CAP_ prefix uppercase accepted",
			input:  "CAP_NET_BIND_SERVICE",
			expVal: unix.CAP_NET_BIND_SERVICE,
		},
		{
			name:   "unknown capability",
			input:  "not_a_real_cap",
			expErr: `unknown capability "not_a_real_cap"`,
		},
		{
			name:   "empty string",
			input:  "",
			expErr: `unknown capability ""`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			val, err := ParseCap(tc.input)
			if tc.expErr != "" {
				must.EqError(t, err, tc.expErr)
				must.Eq(t, uintptr(0), val)
			} else {
				must.NoError(t, err)
				must.Eq(t, tc.expVal, val)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Run("empty slice returns empty", func(t *testing.T) {
		result, err := Resolve(nil)
		must.NoError(t, err)
		must.Len(t, 0, result)
	})

	t.Run("valid names resolved", func(t *testing.T) {
		result, err := Resolve([]string{"chown", "net_bind_service"})
		must.NoError(t, err)
		must.Len(t, 2, result)
		must.Eq(t, unix.CAP_CHOWN, result[0])
		must.Eq(t, unix.CAP_NET_BIND_SERVICE, result[1])
	})

	t.Run("first unknown name returns error", func(t *testing.T) {
		_, err := Resolve([]string{"chown", "bad_cap"})
		must.EqError(t, err, `unknown capability "bad_cap"`)
	})
}
